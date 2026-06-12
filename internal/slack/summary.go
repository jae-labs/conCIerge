package slack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jae-labs/concierge/internal/conversation"
	"github.com/slack-go/slack"
)

// replyPRCtx posts the request summary to the public requests channel, sends a
// closing message in the user's thread, and clears the in-memory state.
func (h *Handler) replyPRCtx(ctx context.Context, state *conversation.State, prTitle, prURL string) {
	h.prCreated.Add(ctx, 1)

	current := h.store.Get(state.ThreadTS)
	superseded := current == nil || current.Nonce != state.Nonce

	summary := buildRequestSummary(state, prTitle, prURL)
	msgTS := h.postMessageCtx(ctx, h.requestsChannelID, "post request summary",
		slack.MsgOptionBlocks(summary...),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl())

	var link string
	if msgTS != "" {
		link, _ = h.getPermalink(ctx, &slack.PermalinkParameters{Channel: h.requestsChannelID, Ts: msgTS})
	}
	h.postMessageCtx(ctx, state.ChannelID, "post request closure",
		slack.MsgOptionText(buildClosingText(link, h.requestsChannelID), false),
		slack.MsgOptionTS(state.ThreadTS),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl())

	if !superseded {
		h.store.Delete(state.ThreadTS)
	}
}

func buildClosingText(link, fallbackChannel string) string {
	const tail = "\n\nThis chat has ended. Open a *New Chat* if you need to raise a new request."
	if link != "" {
		return fmt.Sprintf("Request submitted: <%s|View your request>.%s", link, tail)
	}
	return fmt.Sprintf("Request submitted to <#%s>.%s", fallbackChannel, tail)
}

func buildRequestSummary(state *conversation.State, prTitle, prURL string) []slack.Block {
	var sb strings.Builder
	now := time.Now().UTC().Format("2 Jan 2006, 15:04 UTC")
	fmt.Fprintf(&sb, "• *Request:* %s\n", requestSummaryTitle(prTitle))
	fmt.Fprintf(&sb, "• *Requested by:* <@%s>\n", state.UserID)
	fmt.Fprintf(&sb, "• *Requested at:* %s\n", now)
	fmt.Fprintf(&sb, "• *Resource:* %s\n", state.ResourceType)
	if state.TargetRepo != "" {
		fmt.Fprintf(&sb, "• *Target:* %s\n", state.TargetRepo)
	}
	if state.Justification != "" {
		fmt.Fprintf(&sb, "• *Justification:* %s\n", state.Justification)
	}
	fmt.Fprintf(&sb, "• *PR:* <%s|%s>\n", prURL, prLabel(prURL))

	return []slack.Block{slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", sb.String(), false, false),
		nil, nil,
	)}
}

func prLabel(prURL string) string {
	if matches := prURLPattern.FindStringSubmatch(prURL); len(matches) >= 2 {
		return "#" + matches[1]
	}
	return "View PR"
}

func requestSummaryTitle(prTitle string) string {
	return strings.TrimSpace(strings.TrimPrefix(prTitle, "Request:"))
}

// resolveRequesterName looks up the Slack user's display name, falling back to
// the user ID on any failure.
func (h *Handler) resolveRequesterName(ctx context.Context, userID string) string {
	if h.api == nil {
		return userID
	}
	user, err := h.getUserInfo(ctx, userID)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to resolve slack user name", "error", err, "user_id", userID)
		return userID
	}
	if name := strings.TrimSpace(user.RealName); name != "" {
		return name
	}
	if name := strings.TrimSpace(user.Profile.RealName); name != "" {
		return name
	}
	return userID
}

// lockFlowMessages rewrites every tracked interactive message to its locked
// (read-only) variant. Pass a detached context (e.g. via workflowContext) when
// running in a goroutine that should outlive the originating request.
func (h *Handler) lockFlowMessages(ctx context.Context, state *conversation.State) {
	for _, msg := range state.Messages {
		blocks := lockedBlocksFor(msg, state.Category)
		if blocks == nil {
			continue
		}
		h.updateChannelMessageCtx(ctx, state.ChannelID, msg.TS, "lock flow message", slack.MsgOptionBlocks(blocks...))
	}
}

func lockedBlocksFor(msg conversation.TrackedMessage, category string) []slack.Block {
	switch msg.Kind {
	case conversation.MsgCategory:
		return LockedCategoryBlocks(msg.Label)
	case conversation.MsgResource:
		return LockedResourceBlocks(category, msg.Label)
	case conversation.MsgAction:
		return LockedActionBlocks(msg.Label)
	case conversation.MsgConfirmation:
		return LockedConfirmationBlocks()
	case conversation.MsgWelcome:
		return FlowEndedBlocks()
	}
	return nil
}
