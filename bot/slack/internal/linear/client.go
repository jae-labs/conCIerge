package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const apiURL = "https://api.linear.app/graphql"

// Client is a minimal Linear GraphQL API client.
type Client struct {
	apiKey string
	teamID string
	http   *http.Client
}

func NewClient(apiKey, teamID string) *Client {
	apiKey = strings.TrimSpace(apiKey)
	return &Client{
		apiKey: apiKey,
		teamID: teamID,
		http:   &http.Client{},
	}
}

// Issue holds the result of a created issue.
type Issue struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type issueCreateResponse struct {
	Data struct {
		IssueCreate struct {
			Success bool  `json:"success"`
			Issue   Issue `json:"issue"`
		} `json:"issueCreate"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// LinearPriority maps our priority strings to Linear's integer values.
// 0=None, 1=Urgent, 2=High, 3=Medium, 4=Low
func LinearPriority(priority string) int {
	switch priority {
	case "high":
		return 2
	case "medium":
		return 3
	case "low":
		return 4
	default:
		return 4
	}
}

// CreateIssue creates a new issue in the configured team.
func (c *Client) CreateIssue(ctx context.Context, title, description string, priority int) (*Issue, error) {
	query := `mutation IssueCreate($input: IssueCreateInput!) {
		issueCreate(input: $input) {
			success
			issue {
				id
				identifier
				title
				url
			}
		}
	}`

	req := gqlRequest{
		Query: query,
		Variables: map[string]any{
			"input": map[string]any{
				"title":       title,
				"description": description,
				"teamId":      c.teamID,
				"priority":    priority,
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", c.apiKey)
	httpReq.Header.Set("User-Agent", "opsy-bot/1.0")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result issueCreateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("linear API error: %s", result.Errors[0].Message)
	}

	if !result.Data.IssueCreate.Success {
		return nil, fmt.Errorf("linear issue creation failed")
	}

	return &result.Data.IssueCreate.Issue, nil
}
