package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v68/github"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Client struct {
	client          *gh.Client
	owner           string
	repo            string
	tracer          trace.Tracer
	requestsTotal   metric.Int64Counter
	requestDuration metric.Float64Histogram
}

// NewClient creates a GitHub client authenticated as a GitHub App installation.
// Outbound HTTP calls are wrapped with otelhttp for distributed trace propagation.
func NewClient(appID, installationID int64, privateKey []byte, owner, repo string) (*Client, error) {
	transport, err := ghinstallation.New(
		otelhttp.NewTransport(http.DefaultTransport),
		appID, installationID, privateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("create github app transport: %w", err)
	}
	client := gh.NewClient(&http.Client{Transport: transport})
	meter := otel.Meter("concierge/github")
	requestsTotal, _ := meter.Int64Counter("concierge.github.api.calls.total",
		metric.WithDescription("Total GitHub API operations by method and status"),
	)
	requestDuration, _ := meter.Float64Histogram("concierge.github.api.duration.seconds",
		metric.WithDescription("Duration of GitHub API operations"),
	)
	return &Client{
		client:          client,
		owner:           owner,
		repo:            repo,
		tracer:          otel.Tracer("concierge/github"),
		requestsTotal:   requestsTotal,
		requestDuration: requestDuration,
	}, nil
}

// GetFileContent fetches a file from the default branch.
func (c *Client) GetFileContent(ctx context.Context, path string) (_ []byte, _ string, err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "get_file_content"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.String("file.path", path),
	}
	ctx, span, started := c.startOperation(ctx, "get_file_content", attrs...)
	defer func() {
		c.finishOperation(ctx, span, started, err, attrs...)
	}()

	fileContent, _, resp, err := c.client.Repositories.GetContents(ctx, c.owner, c.repo, path, nil)
	if err != nil {
		return nil, "", fmt.Errorf("get contents: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	content, err := fileContent.GetContent()
	if err != nil {
		return nil, "", fmt.Errorf("decode content: %w", err)
	}
	return []byte(content), fileContent.GetSHA(), nil
}

// CreateBranchFromMain creates a new branch from the HEAD of main.
func (c *Client) CreateBranchFromMain(ctx context.Context, branchName string) (err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "create_branch"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.String("git.branch", branchName),
	}
	ctx, span, started := c.startOperation(ctx, "create_branch", attrs...)
	defer func() {
		c.finishOperation(ctx, span, started, err, attrs...)
	}()

	ref, _, err := c.client.Git.GetRef(ctx, c.owner, c.repo, "refs/heads/main")
	if err != nil {
		return fmt.Errorf("get main ref: %w", err)
	}
	newRef := &gh.Reference{
		Ref:    gh.Ptr("refs/heads/" + branchName),
		Object: &gh.GitObject{SHA: ref.Object.SHA},
	}
	_, _, err = c.client.Git.CreateRef(ctx, c.owner, c.repo, newRef)
	if err != nil {
		// branch already exists — delete it and retry with fresh main HEAD
		_, delErr := c.client.Git.DeleteRef(ctx, c.owner, c.repo, "refs/heads/"+branchName)
		if delErr != nil {
			return fmt.Errorf("create ref: %w (delete old branch also failed: %v)", err, delErr)
		}
		_, _, err = c.client.Git.CreateRef(ctx, c.owner, c.repo, newRef)
		if err != nil {
			return fmt.Errorf("create ref after retry: %w", err)
		}
	}
	return nil
}

// UpdateFile creates or updates a file on a branch.
func (c *Client) UpdateFile(ctx context.Context, branch, path string, content []byte, fileSHA, commitMsg string) (err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "update_file"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.String("git.branch", branch),
		attribute.String("file.path", path),
	}
	ctx, span, started := c.startOperation(ctx, "update_file", attrs...)
	defer func() {
		c.finishOperation(ctx, span, started, err, attrs...)
	}()

	opts := &gh.RepositoryContentFileOptions{
		Message: gh.Ptr(commitMsg),
		Content: content,
		Branch:  gh.Ptr(branch),
		SHA:     gh.Ptr(fileSHA),
		Author: &gh.CommitAuthor{
			Name:  gh.Ptr("conCierge Bot"),
			Email: gh.Ptr("luiz@justanother.engineer"),
		},
	}
	_, _, err = c.client.Repositories.UpdateFile(ctx, c.owner, c.repo, path, opts)
	if err != nil {
		return fmt.Errorf("update file: %w", err)
	}
	return nil
}

// CommentOnPR adds a comment to a pull request.
func (c *Client) CommentOnPR(ctx context.Context, prNumber int, body string) (err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "comment_pr"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.Int("github.pr_number", prNumber),
	}
	ctx, span, started := c.startOperation(ctx, "comment_pr", attrs...)
	defer func() {
		c.finishOperation(ctx, span, started, err, attrs...)
	}()

	comment := &gh.IssueComment{Body: gh.Ptr(body)}
	_, _, err = c.client.Issues.CreateComment(ctx, c.owner, c.repo, prNumber, comment)
	if err != nil {
		return fmt.Errorf("comment on PR: %w", err)
	}
	return nil
}

// CreatePR opens a pull request.
func (c *Client) CreatePR(ctx context.Context, branch, title, body string) (_ string, err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "create_pr"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.String("git.branch", branch),
	}
	ctx, span, started := c.startOperation(ctx, "create_pr", attrs...)
	defer func() {
		c.finishOperation(ctx, span, started, err, attrs...)
	}()

	pr := &gh.NewPullRequest{
		Title:               gh.Ptr(title),
		Head:                gh.Ptr(branch),
		Base:                gh.Ptr("main"),
		Body:                gh.Ptr(body),
		MaintainerCanModify: gh.Ptr(true),
	}
	created, _, err := c.client.PullRequests.Create(ctx, c.owner, c.repo, pr)
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}
	return created.GetHTMLURL(), nil
}

// UpdatePRBody appends text to an existing PR's body.
func (c *Client) UpdatePRBody(ctx context.Context, prNumber int, appendText string) (err error) {
	attrs := []attribute.KeyValue{
		attribute.String("github.operation", "update_pr_body"),
		attribute.String("github.owner", c.owner),
		attribute.String("github.repo", c.repo),
		attribute.Int("github.pr_number", prNumber),
	}
	ctx, span, started := c.startOperation(ctx, "update_pr_body", attrs...)
	defer func() {
		c.finishOperation(ctx, span, started, err, attrs...)
	}()

	pr, _, err := c.client.PullRequests.Get(ctx, c.owner, c.repo, prNumber)
	if err != nil {
		return fmt.Errorf("get PR: %w", err)
	}
	newBody := pr.GetBody() + "\n\n" + appendText
	_, _, err = c.client.PullRequests.Edit(ctx, c.owner, c.repo, prNumber, &gh.PullRequest{
		Body: gh.Ptr(newBody),
	})
	if err != nil {
		return fmt.Errorf("update PR body: %w", err)
	}
	return nil
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
