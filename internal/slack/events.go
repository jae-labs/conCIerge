package slack

import (
	"context"
	"strings"

	"github.com/jae-labs/concierge/internal/conversation"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const msgUnauthorized = "You are not authorized to use conCierge. Contact an admin for access."

func (h *Handler) handleSocketEventsAPI(evt socketmode.Event) {
	eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}
	h.ackRequest(evt.Request)
	h.dispatchEventsAPIEvent(context.Background(), eventsAPI)
}

// dispatchEventsAPIEvent routes to the test seam first to keep handler logic
// observable from tests without re-implementing the whole pipeline.
func (h *Handler) dispatchEventsAPIEvent(ctx context.Context, eventsAPI slackevents.EventsAPIEvent) {
	if h.eventsAPIProcessor != nil {
		h.eventsAPIProcessor(eventsAPI)
		return
	}
	h.handleEventsAPIEvent(ctx, eventsAPI)
}

func (h *Handler) handleEventsAPIEvent(ctx context.Context, eventsAPI slackevents.EventsAPIEvent) {
	ctx, span := h.tracer.Start(ctx, "slack.events_api_event",
		trace.WithAttributes(attribute.String("event_type", eventsAPI.InnerEvent.Type)),
	)
	defer span.End()
	h.logger.DebugContext(ctx, "events API received", "inner_type", eventsAPI.InnerEvent.Type)
	h.eventsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("event_type", eventsAPI.InnerEvent.Type),
	))

	switch ev := eventsAPI.InnerEvent.Data.(type) {
	case *slackevents.AppHomeOpenedEvent:
		h.handleAppHomeOpened(ctx, ev)
	case *slackevents.AssistantThreadStartedEvent:
		h.handleAssistantThreadStarted(ctx, ev)
	case *slackevents.MessageEvent:
		if ev.ChannelType != "im" || ev.BotID != "" || ev.SubType != "" {
			return
		}
		if ev.ThreadTimeStamp != "" {
			h.handleThreadReply(ctx, ev.Channel, ev.Text, ev.ThreadTimeStamp)
			return
		}
		// top-level DM: treat as its own thread (fallback when assistant_thread_started isn't fired)
		h.handleNewFlow(ctx, ev.User, ev.Channel, ev.TimeStamp)
	}
}

// handleAppHomeOpened publishes the Home tab view when a user opens the app.
func (h *Handler) handleAppHomeOpened(ctx context.Context, ev *slackevents.AppHomeOpenedEvent) {
	if ev.Tab != "home" {
		return
	}
	ctx, span := h.tracer.Start(ctx, "slack.app_home_opened",
		trace.WithAttributes(attribute.String("slack.user_id", ev.User)),
	)
	defer span.End()

	view := slack.HomeTabViewRequest{
		Type:   slack.VTHomeTab,
		Blocks: slack.Blocks{BlockSet: HomeTabBlocks(ev.User)},
	}
	if err := h.publishView(ctx, ev.User, view); err != nil {
		h.logger.ErrorContext(ctx, "failed to publish home tab", "error", err, "user", ev.User)
	}
}

// handleAssistantThreadStarted creates a new flow when the user clicks New Chat.
func (h *Handler) handleAssistantThreadStarted(ctx context.Context, ev *slackevents.AssistantThreadStartedEvent) {
	h.startWelcomeFlow(ctx, ev.AssistantThread.UserID, ev.AssistantThread.ChannelID, ev.AssistantThread.ThreadTimeStamp, "notify unauthorized assistant thread")
}

// handleNewFlow starts a fresh flow threaded to the user's top-level message,
// used as fallback when assistant_thread_started is not available.
func (h *Handler) handleNewFlow(ctx context.Context, userID, channelID, messageTS string) {
	h.startWelcomeFlow(ctx, userID, channelID, messageTS, "notify unauthorized new flow")
}

func (h *Handler) startWelcomeFlow(ctx context.Context, userID, channelID, threadTS, denyOp string) {
	if !h.isAuthorized(userID) {
		h.postMessageCtx(ctx, channelID, denyOp,
			slack.MsgOptionText(msgUnauthorized, false),
			slack.MsgOptionTS(threadTS))
		return
	}
	state := h.store.Create(threadTS, channelID, userID)
	ts := h.postMessageCtx(ctx, channelID, "send welcome",
		slack.MsgOptionBlocks(h.welcomeBlocksCtx(ctx, userID)...),
		slack.MsgOptionTS(threadTS),
	)
	if ts == "" {
		return
	}
	state.TrackMessage(ts, conversation.MsgWelcome, "")
}

// handleThreadReply ends the flow on "cancel" or any free-text input.
func (h *Handler) handleThreadReply(ctx context.Context, channelID, text, threadTS string) {
	state := h.store.Get(threadTS)
	if state == nil {
		h.postMessageCtx(ctx, channelID, "notify expired thread",
			slack.MsgOptionText("This session is no longer valid. Please open a new chat.", false),
			slack.MsgOptionTS(threadTS))
		return
	}

	op := "notify invalid thread reply"
	if strings.EqualFold(strings.TrimSpace(text), "cancel") {
		op = "confirm cancelled thread"
	}
	h.postMessageCtx(ctx, channelID, op,
		slack.MsgOptionText("This session is no longer valid. Please open a new chat.", false),
		slack.MsgOptionTS(threadTS))
	go h.lockFlowMessages(workflowContext(ctx), state)
	h.store.Delete(threadTS)
}

// welcomeBlocksCtx renders the welcome message based on a freshly refreshed
// runtime schema, falling back to the cached schema or a static "unavailable"
// banner when the schema cannot be loaded.
func (h *Handler) welcomeBlocksCtx(ctx context.Context, userID string) []slack.Block {
	runtimeSchema, err := h.refreshRuntimeSchema(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to reload runtime schema", "error", err)
		h.schemaMu.RLock()
		runtimeSchema = h.runtimeSchema
		h.schemaMu.RUnlock()
		if runtimeSchema == nil {
			return WelcomeBlocks(userID)
		}
	}
	return WelcomeBlocksFromCategories(userID, CategoryOptionsFromSchema(runtimeSchema.Categories))
}
