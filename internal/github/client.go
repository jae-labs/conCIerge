package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v84/github"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultBranch         = "main"
	defaultBranchRef      = "refs/heads/" + defaultBranch
	defaultCommitAuthor   = "conCierge Bot"
	defaultCommitAuthorEm = "239121271+luiz1361@users.noreply.github.com"
)

// Author identifies the commit author the bot writes as.
type Author struct {
	Name  string
	Email string
}

// DefaultAuthor returns the noreply-style author used when no override is configured.
func DefaultAuthor() Author {
	return Author{Name: defaultCommitAuthor, Email: defaultCommitAuthorEm}
}

type Client struct {
	client          *gh.Client
	owner           string
	repo            string
	author          Author
	tracer          trace.Tracer
	requestsTotal   metric.Int64Counter
	requestDuration metric.Float64Histogram
}

// NewClient creates a GitHub client authenticated as a GitHub App installation.
// Outbound HTTP is wrapped with otelhttp for distributed trace propagation.
// A zero-value Author falls back to DefaultAuthor().
func NewClient(appID, installationID int64, privateKey []byte, owner, repo string, author Author) (*Client, error) {
	transport, err := ghinstallation.New(
		otelhttp.NewTransport(http.DefaultTransport),
		appID, installationID, privateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("create github app transport: %w", err)
	}
	if author.Name == "" {
		author.Name = defaultCommitAuthor
	}
	if author.Email == "" {
		author.Email = defaultCommitAuthorEm
	}
	meter := otel.Meter("concierge/github")
	requestsTotal, _ := meter.Int64Counter("concierge.github.api.calls.total",
		metric.WithDescription("Total GitHub API operations by method and status"),
	)
	requestDuration, _ := meter.Float64Histogram("concierge.github.api.duration.seconds",
		metric.WithDescription("Duration of GitHub API operations"),
	)
	return &Client{
		client:          gh.NewClient(&http.Client{Transport: transport}),
		owner:           owner,
		repo:            repo,
		author:          author,
		tracer:          otel.Tracer("concierge/github"),
		requestsTotal:   requestsTotal,
		requestDuration: requestDuration,
	}, nil
}

// GetFileContent fetches a file from the default branch.
func (c *Client) GetFileContent(ctx context.Context, path string) (content []byte, sha string, err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "get_file_content"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.String("file.path", path),
	}
	ctx, span, started := c.startOperation(ctx, "get_file_content", attrs...)
	defer func() { c.finishOperation(ctx, span, started, err, attrs...) }()

	file, _, resp, err := c.client.Repositories.GetContents(ctx, c.owner, c.repo, path, nil)
	if err != nil {
		return nil, "", fmt.Errorf("get contents: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	decoded, err := file.GetContent()
	if err != nil {
		return nil, "", fmt.Errorf("decode content: %w", err)
	}
	return []byte(decoded), file.GetSHA(), nil
}

// CreateBranchFromMain creates a branch from the current HEAD of main, replacing
// an existing branch with the same name when present.
func (c *Client) CreateBranchFromMain(ctx context.Context, branchName string) (err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "create_branch"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.String("git.branch", branchName),
	}
	ctx, span, started := c.startOperation(ctx, "create_branch", attrs...)
	defer func() { c.finishOperation(ctx, span, started, err, attrs...) }()

	ref, _, err := c.client.Git.GetRef(ctx, c.owner, c.repo, defaultBranchRef)
	if err != nil {
		return fmt.Errorf("get main ref: %w", err)
	}
	newRef := gh.CreateRef{
		Ref: "refs/heads/" + branchName,
		SHA: ref.Object.GetSHA(),
	}
	if _, _, err = c.client.Git.CreateRef(ctx, c.owner, c.repo, newRef); err == nil {
		return nil
	}
	// branch likely exists; delete and retry from fresh main HEAD
	_, delErr := c.client.Git.DeleteRef(ctx, c.owner, c.repo, "refs/heads/"+branchName)
	if delErr != nil {
		return errors.Join(
			fmt.Errorf("create ref: %w", err),
			fmt.Errorf("delete old branch: %w", delErr),
		)
	}
	if _, _, err = c.client.Git.CreateRef(ctx, c.owner, c.repo, newRef); err != nil {
		return fmt.Errorf("create ref after retry: %w", err)
	}
	return nil
}

// UpdateFile commits a file change on the given branch.
func (c *Client) UpdateFile(ctx context.Context, branch, path string, content []byte, fileSHA, commitMsg string) (err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "update_file"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.String("git.branch", branch),
		attribute.String("file.path", path),
	}
	ctx, span, started := c.startOperation(ctx, "update_file", attrs...)
	defer func() { c.finishOperation(ctx, span, started, err, attrs...) }()

	opts := &gh.RepositoryContentFileOptions{
		Message: gh.Ptr(commitMsg),
		Content: content,
		Branch:  gh.Ptr(branch),
		SHA:     gh.Ptr(fileSHA),
		Author: &gh.CommitAuthor{
			Name:  gh.Ptr(c.author.Name),
			Email: gh.Ptr(c.author.Email),
		},
	}
	if _, _, err = c.client.Repositories.UpdateFile(ctx, c.owner, c.repo, path, opts); err != nil {
		return fmt.Errorf("update file: %w", err)
	}
	return nil
}

// CreatePR opens a pull request from branch into main and returns its HTML URL.
func (c *Client) CreatePR(ctx context.Context, branch, title, body string) (url string, err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "create_pr"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.String("git.branch", branch),
	}
	ctx, span, started := c.startOperation(ctx, "create_pr", attrs...)
	defer func() { c.finishOperation(ctx, span, started, err, attrs...) }()

	pr := &gh.NewPullRequest{
		Title:               gh.Ptr(title),
		Head:                gh.Ptr(branch),
		Base:                gh.Ptr(defaultBranch),
		Body:                gh.Ptr(body),
		MaintainerCanModify: gh.Ptr(true),
	}
	created, _, err := c.client.PullRequests.Create(ctx, c.owner, c.repo, pr)
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}
	return created.GetHTMLURL(), nil
}

func (c *Client) startOperation(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span, time.Time) {
	ctx, span := c.tracer.Start(ctx, "github."+name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx, span, time.Now()
}

func (c *Client) finishOperation(ctx context.Context, span trace.Span, started time.Time, err error, attrs ...attribute.KeyValue) {
	outcome := "ok"
	if err != nil {
		outcome = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	attrs = append(attrs, attribute.String("outcome", outcome))
	c.requestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	c.requestDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
	span.End()
}
