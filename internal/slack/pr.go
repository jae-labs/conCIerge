package slack

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jae-labs/concierge/internal/conversation"
	ghclient "github.com/jae-labs/concierge/internal/github"
	hcleditor "github.com/jae-labs/concierge/internal/hcl"
	"github.com/jae-labs/concierge/internal/schema"
	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const branchPrefix = "concierge/"

// prArtifacts captures everything needed to push a PR for one dynamic flow.
type prArtifacts struct {
	Modified  []byte
	Branch    string
	CommitMsg string
	Title     string
}

// createDynamicPR drives the end-to-end Terraform mutation + PR creation for a
// completed flow. All progress, errors, and the final summary are mirrored to
// the user's Slack thread.
func (h *Handler) createDynamicPR(parent context.Context, state *conversation.State) {
	ctx, span, started := h.startWorkflow(parent, "create_dynamic_pr")
	var workflowErr error
	defer func() { h.finishWorkflow(ctx, span, started, workflowErr) }()

	h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Fetching Terraform files...", false))

	resource, err := h.fetchSchema(ctx, state.ResourceType)
	if err != nil {
		workflowErr = h.failPR(ctx, state, "fetch schema", "Failed to load schema", err)
		return
	}

	src, fileSHA, err := h.gh.GetFileContent(ctx, resource.File)
	if err != nil {
		workflowErr = h.failPR(ctx, state, "fetch terraform file", "Failed to fetch file", err)
		return
	}

	artifacts, err := buildPRArtifacts(resource, state, src)
	if err != nil {
		workflowErr = h.failPR(ctx, state, "edit hcl", "HCL editing failed", err)
		return
	}

	h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Creating GitHub branch and committing changes...", false))

	if err := h.gh.CreateBranchFromMain(ctx, artifacts.Branch); err != nil {
		workflowErr = h.failPR(ctx, state, "create branch", "Failed to create branch", err)
		return
	}
	if err := h.gh.UpdateFile(ctx, artifacts.Branch, resource.File, artifacts.Modified, fileSHA, artifacts.CommitMsg); err != nil {
		workflowErr = h.failPR(ctx, state, "commit terraform update", "Failed to commit changes", err)
		return
	}

	requester := h.resolveRequesterName(ctx, state.UserID)
	prDesc := fmt.Sprintf("Requested by %s\nJustification: %s", requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, artifacts.Branch, artifacts.Title, prDesc)
	if err != nil {
		workflowErr = h.failPR(ctx, state, "create pull request", "Failed to create PR", err)
		return
	}

	h.prCreated.Add(ctx, 1, metric.WithAttributes(
		attribute.String("resource_type", state.ResourceType),
		attribute.String("action", state.ActionType),
	))
	h.replyPRCtx(ctx, state, artifacts.Title, prURL)
	h.store.Delete(state.ThreadTS)
}

// failPR posts the failure message in the thread, records the error in Sentry/
// span context, and returns the original error so the caller can mark the
// workflow as failed.
func (h *Handler) failPR(ctx context.Context, state *conversation.State, step, userMsg string, err error) error {
	captureWorkflowError(ctx, state, step, err)
	h.replyCtx(ctx, state, conversation.MsgProgress, "",
		slack.MsgOptionText(fmt.Sprintf("%s: %v", userMsg, err), false))
	return err
}

// buildPRArtifacts dispatches HCL mutation and PR metadata generation by kind
// and action.
func buildPRArtifacts(resource *schema.Resource, state *conversation.State, src []byte) (prArtifacts, error) {
	timestamp := time.Now().Unix()
	sanitizedKey := ghclient.SanitizeBranchSegment(state.TargetRepo)
	if sanitizedKey == "" {
		sanitizedKey = ghclient.SanitizeBranchSegment(resource.ID)
	}

	switch resource.Kind {
	case schema.KindSingleton:
		return buildSingletonArtifacts(resource, state, src, sanitizedKey, timestamp)
	case schema.KindMembership:
		return buildMembershipArtifacts(resource, state, src, sanitizedKey, timestamp)
	default:
		return buildMapEntryArtifacts(resource, state, src, sanitizedKey, timestamp)
	}
}

func buildSingletonArtifacts(resource *schema.Resource, state *conversation.State, src []byte, key string, ts int64) (prArtifacts, error) {
	modified, err := hcleditor.UpdateSingleton(src, resource.RootPath, state.DynamicConfig, resource)
	if err != nil {
		return prArtifacts{}, err
	}
	return prArtifacts{
		Modified:  modified,
		Branch:    fmt.Sprintf("%supdate-%s-%s-%d", branchPrefix, state.ResourceType, key, ts),
		CommitMsg: fmt.Sprintf("Update %s", state.ResourceType),
		Title:     fmt.Sprintf("concierge: Update %s", state.ResourceType),
	}, nil
}

func buildMembershipArtifacts(resource *schema.Resource, state *conversation.State, src []byte, key string, ts int64) (prArtifacts, error) {
	team, _ := state.DynamicConfig["team"].(string)
	username, _ := state.DynamicConfig["username"].(string)
	role, _ := state.DynamicConfig["role"].(string)

	modified, err := hcleditor.ApplyMembershipAction(src, state.DynamicConfig, state.ActionType, resource)
	if err != nil {
		return prArtifacts{}, err
	}

	switch state.ActionType {
	case schema.ActionAdd:
		return prArtifacts{
			Modified:  modified,
			Branch:    fmt.Sprintf("%sadd-%s-%s-%d", branchPrefix, state.ResourceType, key, ts),
			CommitMsg: fmt.Sprintf("Add %s to %s as %s", username, team, role),
			Title:     fmt.Sprintf("concierge: Add %s to %s", username, team),
		}, nil
	case schema.ActionDelete:
		return prArtifacts{
			Modified:  modified,
			Branch:    fmt.Sprintf("%sdelete-%s-%s-%d", branchPrefix, state.ResourceType, key, ts),
			CommitMsg: fmt.Sprintf("Remove %s from %s", username, team),
			Title:     fmt.Sprintf("concierge: Remove %s from %s", username, team),
		}, nil
	case schema.ActionChangeRole:
		return prArtifacts{
			Modified:  modified,
			Branch:    fmt.Sprintf("%schange-role-%s-%s-%d", branchPrefix, state.ResourceType, key, ts),
			CommitMsg: fmt.Sprintf("Change %s role in %s to %s", username, team, role),
			Title:     fmt.Sprintf("concierge: Change %s role in %s", username, team),
		}, nil
	}
	return prArtifacts{}, fmt.Errorf("unsupported membership action %q", state.ActionType)
}

func buildMapEntryArtifacts(resource *schema.Resource, state *conversation.State, src []byte, key string, ts int64) (prArtifacts, error) {
	switch state.ActionType {
	case schema.ActionAdd:
		modified, err := hcleditor.AddResource(src, resource.RootPath, state.TargetRepo, state.DynamicConfig, resource)
		if err != nil {
			return prArtifacts{}, err
		}
		return prArtifacts{
			Modified:  modified,
			Branch:    fmt.Sprintf("%sadd-%s-%s-%d", branchPrefix, state.ResourceType, key, ts),
			CommitMsg: fmt.Sprintf("Add %s %s", state.ResourceType, state.TargetRepo),
			Title:     fmt.Sprintf("concierge: Add %s %s", state.ResourceType, state.TargetRepo),
		}, nil
	case schema.ActionDelete:
		modified, err := hcleditor.RemoveResource(src, resource.RootPath, state.TargetRepo)
		if err != nil {
			return prArtifacts{}, err
		}
		return prArtifacts{
			Modified:  modified,
			Branch:    fmt.Sprintf("%sdelete-%s-%s-%d", branchPrefix, state.ResourceType, key, ts),
			CommitMsg: fmt.Sprintf("Remove %s %s", state.ResourceType, state.TargetRepo),
			Title:     fmt.Sprintf("concierge: Remove %s %s", state.ResourceType, state.TargetRepo),
		}, nil
	case schema.ActionSettings:
		modified, err := hcleditor.UpdateResource(src, resource.RootPath, state.TargetRepo, state.DynamicConfig, resource)
		if err != nil {
			return prArtifacts{}, err
		}
		return prArtifacts{
			Modified:  modified,
			Branch:    fmt.Sprintf("%supdate-%s-%s-%d", branchPrefix, state.ResourceType, key, ts),
			CommitMsg: fmt.Sprintf("Update %s %s", state.ResourceType, state.TargetRepo),
			Title:     fmt.Sprintf("concierge: Update %s %s", state.ResourceType, state.TargetRepo),
		}, nil
	}
	return prArtifacts{}, fmt.Errorf("unsupported action %q", state.ActionType)
}

// showDynamicConfirmation renders the per-action confirmation message and a
// "processing" follow-up in the user's thread before PR creation begins.
func (h *Handler) showDynamicConfirmation(ctx context.Context, state *conversation.State, resource *schema.Resource) {
	targetLabel := state.TargetRepo
	if resource.Kind == schema.KindSingleton {
		targetLabel = resource.Label
	}

	var summary string
	switch state.ActionType {
	case schema.ActionDelete:
		summary = fmt.Sprintf("*Action*: Delete %s\n*Target*: `%s`\n*Justification*: %s",
			resource.Label, targetLabel, state.Justification)
	case schema.ActionAdd:
		summary = buildCreateSummary(resource, state, targetLabel)
	default:
		summary = h.buildUpdateSummary(ctx, resource, state, targetLabel)
	}

	h.replyCtx(ctx, state, conversation.MsgProgress, "",
		slack.MsgOptionText(fmt.Sprintf("*Request summary:*\n%s", summary), false))
	h.replyCtx(ctx, state, conversation.MsgProgress, "",
		slack.MsgOptionText("Processing your request...", false))
}

func buildCreateSummary(resource *schema.Resource, state *conversation.State, targetLabel string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "*Action*: Create %s\n*Target*: `%s`\n", resource.Label, targetLabel)
	keys := make([]string, 0, len(state.DynamicConfig))
	for k := range state.DynamicConfig {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "*%s*: `%v`\n", k, state.DynamicConfig[k])
	}
	return sb.String()
}

func (h *Handler) buildUpdateSummary(ctx context.Context, resource *schema.Resource, state *conversation.State, targetLabel string) string {
	var oldValues map[string]any
	switch resource.Kind {
	case schema.KindSingleton:
		oldValues, _ = h.fetchExistingDynamicValues(ctx, resource, "")
	case schema.KindMapEntry:
		oldValues, _ = h.fetchExistingDynamicValues(ctx, resource, state.TargetRepo)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "*Action*: Update %s\n*Target*: `%s`\n*Justification*: %s\n\n*Changes*:\n",
		resource.Label, targetLabel, state.Justification)
	for _, step := range resource.Steps {
		for _, field := range step.Fields {
			newVal := state.DynamicConfig[field.Path]
			oldVal := oldValues[field.Path]
			if fmt.Sprintf("%v", newVal) != fmt.Sprintf("%v", oldVal) {
				fmt.Fprintf(&sb, "~ `%s`: `%v` -> `%v`\n", field.Path, oldVal, newVal)
			}
		}
	}
	return sb.String()
}
