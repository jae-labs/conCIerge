package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v68/github"
)

type Client struct {
	client *gh.Client
	owner  string
	repo   string
}

// NewClient creates a GitHub client authenticated as a GitHub App installation.
func NewClient(appID, installationID int64, privateKey []byte, owner, repo string) (*Client, error) {
	transport, err := ghinstallation.New(http.DefaultTransport, appID, installationID, privateKey)
	if err != nil {
		return nil, fmt.Errorf("create github app transport: %w", err)
	}
	client := gh.NewClient(&http.Client{Transport: transport})
	return &Client{client: client, owner: owner, repo: repo}, nil
}

// GetFileContent fetches a file from the default branch.
func (c *Client) GetFileContent(ctx context.Context, path string) ([]byte, string, error) {
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
func (c *Client) CreateBranchFromMain(ctx context.Context, branchName string) error {
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
func (c *Client) UpdateFile(ctx context.Context, branch, path string, content []byte, fileSHA, commitMsg string) error {
	opts := &gh.RepositoryContentFileOptions{
		Message: gh.Ptr(commitMsg),
		Content: content,
		Branch:  gh.Ptr(branch),
		SHA:     gh.Ptr(fileSHA),
		Author: &gh.CommitAuthor{
			Name:  gh.Ptr("Opsy Bot"),
			Email: gh.Ptr("luiz@justanother.engineer"),
		},
	}
	_, _, err := c.client.Repositories.UpdateFile(ctx, c.owner, c.repo, path, opts)
	if err != nil {
		return fmt.Errorf("update file: %w", err)
	}
	return nil
}

// CommentOnPR adds a comment to a pull request.
func (c *Client) CommentOnPR(ctx context.Context, prNumber int, body string) error {
	comment := &gh.IssueComment{Body: gh.Ptr(body)}
	_, _, err := c.client.Issues.CreateComment(ctx, c.owner, c.repo, prNumber, comment)
	if err != nil {
		return fmt.Errorf("comment on PR: %w", err)
	}
	return nil
}

// CreatePR opens a pull request.
func (c *Client) CreatePR(ctx context.Context, branch, title, body string) (string, error) {
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
func (c *Client) UpdatePRBody(ctx context.Context, prNumber int, appendText string) error {
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
