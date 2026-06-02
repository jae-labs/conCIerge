package slack

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jae-labs/conCIerge/internal/conversation"
	ghclient "github.com/jae-labs/conCIerge/internal/github"
	hcleditor "github.com/jae-labs/conCIerge/internal/hcl"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	pathGitHubRepos     = "github/locals_repos.tf"
	pathGitHubMembers   = "github/locals_members.tf"
	pathGitHubOrg       = "github/locals_org.tf"
	pathCloudflareDNS   = "cloudflare/locals_dns.tf"
	pathDopplerProjects = "doppler/locals_projects.tf"
)

var prURLPattern = regexp.MustCompile(`/pull/(\d+)`)

// isAuthorized checks if a user can use the bot.
func (h *Handler) isAuthorized(userID string) bool {
	return h.userIDs[userID]
}

type Handler struct {
	api                *slack.Client
	sm                 *socketmode.Client
	store              *conversation.Store
	gh                 *ghclient.Client
	eventsAPIProcessor func(slackevents.EventsAPIEvent)
	logger             *slog.Logger
	requestsChannelID  string
	userIDs            map[string]bool

	// Observability instruments — pre-created at construction; safe to call on
	// noop instruments when OTel is not configured.
	tracer           trace.Tracer
	eventsTotal      metric.Int64Counter
	prCreated        metric.Int64Counter
	slackAPITotal    metric.Int64Counter
	slackAPIDuration metric.Float64Histogram
	workflowTotal    metric.Int64Counter
	workflowDuration metric.Float64Histogram
}

func NewHandler(api *slack.Client, gh *ghclient.Client, requestsChannelID string, userIDs map[string]bool, logger *slog.Logger) *Handler {
	m := otel.Meter("concierge/slack")
	eventsTotal, _ := m.Int64Counter("concierge.slack.events.total",
		metric.WithDescription("Total Slack events dispatched by inner event type"),
	)
	prCreated, _ := m.Int64Counter("concierge.slack.pr.created.total",
		metric.WithDescription("Total PRs created by resource type and action"),
	)
	slackAPITotal, _ := m.Int64Counter("concierge.slack.api.calls.total",
		metric.WithDescription("Total outbound Slack Web API calls by method and outcome"),
	)
	slackAPIDuration, _ := m.Float64Histogram("concierge.slack.api.duration.seconds",
		metric.WithDescription("Duration of outbound Slack Web API calls"),
	)
	workflowTotal, _ := m.Int64Counter("concierge.slack.workflow.total",
		metric.WithDescription("Total completed Slack workflows by name and outcome"),
	)
	workflowDuration, _ := m.Float64Histogram("concierge.slack.workflow.duration.seconds",
		metric.WithDescription("Duration of completed Slack workflows"),
	)
	return &Handler{
		api:               api,
		store:             conversation.NewStore(),
		gh:                gh,
		requestsChannelID: requestsChannelID,
		userIDs:           userIDs,
		logger:            logger,
		tracer:            otel.Tracer("concierge/slack"),
		eventsTotal:       eventsTotal,
		prCreated:         prCreated,
		slackAPITotal:     slackAPITotal,
		slackAPIDuration:  slackAPIDuration,
		workflowTotal:     workflowTotal,
		workflowDuration:  workflowDuration,
	}
}

type interactionResponder interface {
	Ack(payload ...any) error
}

type interactionResponderFunc func(payload ...any) error

func (f interactionResponderFunc) Ack(payload ...any) error {
	return f(payload...)
}

func (h *Handler) RunSocketMode(ctx context.Context, sm *socketmode.Client) error {
	h.sm = sm
	go h.eventLoop()
	return h.sm.RunContext(ctx)
}

func (h *Handler) ackRequest(req *socketmode.Request, payload ...any) {
	if h.sm == nil || req == nil {
		return
	}
	if err := h.sm.Ack(*req, payload...); err != nil {
		h.logger.ErrorContext(context.Background(), "failed to acknowledge socket mode request", "error", err)
	}
}

func (h *Handler) replyCtx(ctx context.Context, state *conversation.State, kind conversation.MessageKind, label string, opts ...slack.MsgOption) string {
	opts = append(opts, slack.MsgOptionTS(state.ThreadTS))
	ts := h.postMessageCtx(ctx, state.ChannelID, "reply", opts...)
	if ts != "" {
		state.TrackMessage(ts, kind, label)
	}
	return ts
}

func (h *Handler) replyPRCtx(ctx context.Context, state *conversation.State, prTitle, prURL string) {
	h.prCreated.Add(ctx, 1, metric.WithAttributes(
		attribute.String("resource_type", state.ResourceType),
		attribute.String("action", state.ActionType),
	))

	current := h.store.Get(state.ThreadTS)
	superseded := current == nil || current.Nonce != state.Nonce

	summary := buildRequestSummary(state, prTitle, prURL)
	msgTS := h.postMessageCtx(ctx, h.requestsChannelID, "post request summary",
		slack.MsgOptionBlocks(summary...),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl())

	var link string
	if msgTS != "" {
		permalink, err := h.getPermalink(ctx, &slack.PermalinkParameters{
			Channel: h.requestsChannelID,
			Ts:      msgTS,
		})
		if err == nil {
			link = permalink
		}
	}

	var closingText string
	if link != "" {
		closingText = fmt.Sprintf("Request submitted: <%s|View your request>.\n\nThis chat has ended. Open a *New Chat* if you need to raise a new request.", link)
	} else {
		closingText = fmt.Sprintf("Request submitted to <#%s>.\n\nThis chat has ended. Open a *New Chat* if you need to raise a new request.", h.requestsChannelID)
	}
	h.postMessageCtx(ctx, state.ChannelID, "post request closure",
		slack.MsgOptionText(closingText, false),
		slack.MsgOptionTS(state.ThreadTS),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl())

	if !superseded {
		h.store.Delete(state.ThreadTS)
	}
}

func (h *Handler) updateMessageCtx(ctx context.Context, state *conversation.State, messageTS string, opts ...slack.MsgOption) {
	h.updateChannelMessageCtx(ctx, state.ChannelID, messageTS, "update interactive message", opts...)
}

func (h *Handler) startWorkflow(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span, time.Time) {
	ctx, span := h.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, span, time.Now()
}

func (h *Handler) finishWorkflow(ctx context.Context, span trace.Span, started time.Time, err error, attrs ...attribute.KeyValue) {
	outcome := "ok"
	if err != nil {
		outcome = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	attrs = append(attrs, attribute.String("outcome", outcome))
	h.workflowTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	h.workflowDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
	span.End()
}

func (h *Handler) workflowAttrs(name string, state *conversation.State) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attribute.String("workflow", name)}
	if state != nil {
		attrs = append(attrs,
			attribute.String("resource_type", state.ResourceType),
			attribute.String("action", state.ActionType),
			attribute.String("slack.user_id", state.UserID),
		)
	}
	return attrs
}

func workflowNameForState(state *conversation.State) string {
	if state == nil {
		return "slack.workflow.unknown"
	}
	return fmt.Sprintf("slack.workflow.%s.%s", state.ResourceType, state.ActionType)
}

func (h *Handler) eventLoop() {
	for evt := range h.sm.Events {
		switch evt.Type {
		case socketmode.EventTypeEventsAPI:
			h.handleSocketEventsAPI(evt)
		case socketmode.EventTypeInteractive:
			h.handleSocketInteractive(evt)
		default:
			h.logger.Debug("unhandled event type", "type", evt.Type)
		}
	}
}

func (h *Handler) handleSocketEventsAPI(evt socketmode.Event) {
	eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}
	h.ackRequest(evt.Request)

	h.dispatchEventsAPIEvent(context.Background(), eventsAPI)
}

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
			h.handleThreadReply(ctx, ev.User, ev.Channel, ev.Text, ev.ThreadTimeStamp)
		} else {
			// top-level DM: treat as its own thread (fallback when assistant threads aren't active)
			h.handleNewFlow(ctx, ev.User, ev.Channel, ev.TimeStamp)
		}
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
		Type: slack.VTHomeTab,
		Blocks: slack.Blocks{
			BlockSet: HomeTabBlocks(ev.User),
		},
	}
	if err := h.publishView(ctx, ev.User, view); err != nil {
		h.logger.ErrorContext(ctx, "failed to publish home tab", "error", err, "user", ev.User)
	}
}

// handleAssistantThreadStarted creates a new flow when user clicks "New Chat".
func (h *Handler) handleAssistantThreadStarted(ctx context.Context, ev *slackevents.AssistantThreadStartedEvent) {
	threadTS := ev.AssistantThread.ThreadTimeStamp
	channelID := ev.AssistantThread.ChannelID
	userID := ev.AssistantThread.UserID

	if !h.isAuthorized(userID) {
		h.postMessageCtx(ctx, channelID, "notify unauthorized assistant thread",
			slack.MsgOptionText("You are not authorized to use conCierge. Contact an admin for access.", false),
			slack.MsgOptionTS(threadTS))
		return
	}

	state := h.store.Create(threadTS, channelID, userID)

	ts := h.postMessageCtx(ctx, channelID, "send welcome",
		slack.MsgOptionBlocks(WelcomeBlocks(userID)...),
		slack.MsgOptionTS(threadTS),
	)
	if ts == "" {
		return
	}
	state.TrackMessage(ts, conversation.MsgWelcome, "")
}

// handleNewFlow starts a fresh flow threaded to the user's top-level message.
// Used as fallback when assistant_thread_started is not available.
func (h *Handler) handleNewFlow(ctx context.Context, userID, channelID, messageTS string) {
	if !h.isAuthorized(userID) {
		h.postMessageCtx(ctx, channelID, "notify unauthorized new flow",
			slack.MsgOptionText("You are not authorized to use conCierge. Contact an admin for access.", false),
			slack.MsgOptionTS(messageTS))
		return
	}

	state := h.store.Create(messageTS, channelID, userID)

	ts := h.postMessageCtx(ctx, channelID, "send welcome",
		slack.MsgOptionBlocks(WelcomeBlocks(userID)...),
		slack.MsgOptionTS(messageTS),
	)
	if ts == "" {
		return
	}
	state.TrackMessage(ts, conversation.MsgWelcome, "")
}

// handleThreadReply handles text typed inside a flow's thread (e.g. "cancel").
func (h *Handler) handleThreadReply(ctx context.Context, userID, channelID, text, threadTS string) {
	state := h.store.Get(threadTS)
	if state == nil {
		h.postMessageCtx(ctx, channelID, "notify expired thread",
			slack.MsgOptionText("This session is no longer valid. Please open a new chat.", false),
			slack.MsgOptionTS(threadTS))
		return
	}

	if strings.EqualFold(strings.TrimSpace(text), "cancel") {
		go h.lockFlowMessages(state)
		h.store.Delete(threadTS)
		h.postMessageCtx(ctx, channelID, "confirm cancelled thread",
			slack.MsgOptionText("This session is no longer valid. Please open a new chat.", false),
			slack.MsgOptionTS(threadTS))
		return
	}

	h.postMessageCtx(ctx, channelID, "notify invalid thread reply",
		slack.MsgOptionText("This session is no longer valid. Please open a new chat.", false),
		slack.MsgOptionTS(threadTS))
	go h.lockFlowMessages(state)
	h.store.Delete(threadTS)
}

func (h *Handler) handleSocketInteractive(evt socketmode.Event) {
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		return
	}

	responder := interactionResponderFunc(func(payload ...any) error {
		h.ackRequest(evt.Request, payload...)
		return nil
	})
	h.handleInteractiveCallback(context.Background(), callback, responder)
}

func (h *Handler) handleInteractiveCallback(ctx context.Context, callback slack.InteractionCallback, responder interactionResponder) {
	switch callback.Type {
	case slack.InteractionTypeBlockActions:
		_ = responder.Ack()
		h.handleBlockAction(ctx, callback)
	case slack.InteractionTypeViewSubmission:
		h.handleViewSubmission(ctx, callback, responder)
	default:
		_ = responder.Ack()
	}
}

func (h *Handler) handleBlockAction(ctx context.Context, callback slack.InteractionCallback) {
	if len(callback.ActionCallback.BlockActions) == 0 {
		return
	}
	action := callback.ActionCallback.BlockActions[0]

	// resolve flow from thread
	threadTS := callback.Message.ThreadTimestamp
	if threadTS == "" {
		threadTS = callback.Message.Timestamp
	}
	state := h.store.Get(threadTS)

	if state == nil {
		h.postEphemeralCtx(ctx, callback.Channel.ID, callback.User.ID, "notify expired block action",
			slack.MsgOptionText("This flow has expired. Click *New Chat* to start another.", false))
		return
	}

	if callback.User.ID != state.UserID {
		h.postEphemeralCtx(ctx, callback.Channel.ID, callback.User.ID, "notify unauthorized block action",
			slack.MsgOptionText("This session belongs to another user.", false))
		return
	}

	if !h.isAuthorized(callback.User.ID) {
		h.postEphemeralCtx(ctx, callback.Channel.ID, callback.User.ID, "notify unauthorized user",
			slack.MsgOptionText("You are not authorized to use conCierge. Contact an admin for access.", false))
		return
	}

	messageTS := callback.Message.Timestamp

	if !state.HasMessage(messageTS) {
		h.postEphemeralCtx(ctx, callback.Channel.ID, callback.User.ID, "notify stale block action",
			slack.MsgOptionText("This flow has expired. Click *New Chat* to start another.", false))
		return
	}

	switch action.ActionID {
	case ActionCategorySelect:
		h.handleCategorySelect(ctx, state, action.SelectedOption.Value, messageTS)
	case ActionResourceSelect:
		h.handleResourceSelect(ctx, state, action.SelectedOption.Value, callback.TriggerID, messageTS)
	case ActionActionSelect:
		h.handleActionSelect(ctx, state, action.SelectedOption.Value, callback.TriggerID, messageTS)
	}
}

func (h *Handler) handleCategorySelect(ctx context.Context, state *conversation.State, category, messageTS string) {
	state.Category = category
	state.Phase = conversation.PhaseCategorySelected

	blocks := ResourceBlocks(category)
	h.replyCtx(ctx, state, conversation.MsgResource, "", slack.MsgOptionBlocks(blocks...))

	label := labelForValue(categories, category)
	h.updateMessageCtx(ctx, state, messageTS, slack.MsgOptionBlocks(LockedCategoryBlocks(label)...))

	// if no resources defined for this category, ResourceBlocks returns ComingSoon and flow ends
	if _, ok := resourceOptions[category]; !ok {
		h.store.Delete(state.ThreadTS)
	}
}

func (h *Handler) handleResourceSelect(ctx context.Context, state *conversation.State, resource, triggerID, messageTS string) {
	state.ResourceType = resource
	state.Phase = conversation.PhaseResourceSelected

	switch resource {
	case "repo", "dns", "user_management", "project":
		blocks := ActionBlocks(resource)
		h.replyCtx(ctx, state, conversation.MsgAction, "", slack.MsgOptionBlocks(blocks...))
	case "org_settings":
		h.handleOrgSettingsResource(ctx, state, triggerID)
	default:
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionBlocks(ComingSoonBlocks(resource)...))
		h.store.Delete(state.ThreadTS)
	}

	label := labelForValue(resourceOptions[state.Category], resource)
	h.updateMessageCtx(ctx, state, messageTS, slack.MsgOptionBlocks(LockedResourceBlocks(state.Category, label)...))
}

func (h *Handler) handleActionSelect(ctx context.Context, state *conversation.State, actionType, triggerID, messageTS string) {
	state.ActionType = actionType
	state.Phase = conversation.PhaseActionSelected

	switch state.ResourceType {
	case "dns":
		h.handleDnsAction(ctx, state, actionType, triggerID)
	case "user_management":
		h.handleUserManagementAction(ctx, state, actionType, triggerID)
	case "project":
		h.handleDopplerAction(ctx, state, actionType, triggerID)
	default:
		h.handleRepoAction(ctx, state, actionType, triggerID)
	}

	label := labelForValue(actionOptions[state.ResourceType], actionType)
	h.updateMessageCtx(ctx, state, messageTS, slack.MsgOptionBlocks(LockedActionBlocks(label)...))
}

func (h *Handler) handleRepoAction(ctx context.Context, state *conversation.State, actionType, triggerID string) {
	switch actionType {
	case "add":
		modal := RepoStep1Modal()
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		err := h.openView(ctx, triggerID, modal)
		if err != nil {
			h.logger.Error("failed to open modal", "error", err, "user", state.UserID)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "delete":
		repos := h.fetchRepoNamesCtx(ctx)
		if len(repos) == 0 {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No repositories found. There's nothing to remove.", false))
			return
		}
		modal := DeleteRepoModal(repos)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		err := h.openView(ctx, triggerID, modal)
		if err != nil {
			h.logger.Error("failed to open delete modal", "error", err, "user", state.UserID)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "settings":
		repos := h.fetchRepoNamesCtx(ctx)
		if len(repos) == 0 {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No repositories found. There's nothing to update.", false))
			return
		}
		modal := SelectRepoModal(repos)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		err := h.openView(ctx, triggerID, modal)
		if err != nil {
			h.logger.Error("failed to open select repo modal", "error", err, "user", state.UserID)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	}
}

func (h *Handler) handleDnsAction(ctx context.Context, state *conversation.State, actionType, triggerID string) {
	src, err := h.fetchCloudflareHCLCtx(ctx)
	if err != nil {
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch Cloudflare configuration.", false))
		h.store.Delete(state.ThreadTS)
		return
	}

	zones, err := hcleditor.ExistingZones(src)
	if err != nil || len(zones) == 0 {
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No DNS zones found.", false))
		h.store.Delete(state.ThreadTS)
		return
	}

	// auto-select when only one zone exists
	zone := zones[0]
	state.TargetZone = zone

	switch actionType {
	case "add":
		modal := DnsAddModal(zone)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		if err := h.openView(ctx, triggerID, modal); err != nil {
			h.logger.Error("failed to open dns add modal", "error", err)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "delete":
		records := h.fetchDnsRecordOptions(src, zone)
		if len(records) == 0 {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No DNS records found. There's nothing to remove.", false))
			return
		}
		modal := DnsRemoveModal(zone, records)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		if err := h.openView(ctx, triggerID, modal); err != nil {
			h.logger.Error("failed to open dns remove modal", "error", err)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "settings":
		records := h.fetchDnsRecordOptions(src, zone)
		if len(records) == 0 {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No DNS records found. There's nothing to update.", false))
			return
		}
		modal := DnsSelectRecordModal(zone, records)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		if err := h.openView(ctx, triggerID, modal); err != nil {
			h.logger.Error("failed to open dns select record modal", "error", err)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	}
}

func (h *Handler) handleOrgSettingsResource(ctx context.Context, state *conversation.State, triggerID string) {
	src, err := h.fetchOrgHCLSourceCtx(ctx)
	if err != nil {
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch GitHub configuration.", false))
		h.store.Delete(state.ThreadTS)
		return
	}

	cfg, err := hcleditor.ExtractOrgSettings(src)
	if err != nil {
		h.logger.Error("failed to extract org settings", "error", err)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to read org settings from configuration.", false))
		h.store.Delete(state.ThreadTS)
		return
	}
	state.OrgConfig = cfg

	modal := OrgSettingsModal(state.OrgConfig)
	modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
	if err := h.openView(ctx, triggerID, modal); err != nil {
		h.logger.Error("failed to open org settings modal", "error", err)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
	}
}

func (h *Handler) handleViewSubmission(parent context.Context, callback slack.InteractionCallback, responder interactionResponder) {
	// PrivateMetadata format: "threadTS:nonce"
	parts := strings.SplitN(callback.View.PrivateMetadata, ":", 2)
	if len(parts) != 2 {
		h.logger.WarnContext(parent, "invalid PrivateMetadata format", "metadata", callback.View.PrivateMetadata)
		_ = responder.Ack()
		return
	}
	threadTS, nonce := parts[0], parts[1]

	state := h.store.Get(threadTS)
	if state == nil || state.Nonce != nonce {
		h.logger.WarnContext(parent, "no flow or nonce mismatch for modal submission", "thread_ts", threadTS)
		_ = responder.Ack()
		return
	}

	if callback.User.ID != state.UserID {
		h.logger.WarnContext(parent, "unauthorized modal submission: user mismatch", "expected", state.UserID, "actual", callback.User.ID)
		_ = responder.Ack()
		return
	}

	if !h.isAuthorized(callback.User.ID) {
		h.logger.WarnContext(parent, "unauthorized modal submission: user not authorized", "user", callback.User.ID)
		_ = responder.Ack()
		return
	}

	ctx, span := h.tracer.Start(parent, "slack.view_submission",
		trace.WithAttributes(
			attribute.String("callback_id", callback.View.CallbackID),
			attribute.String("slack.user_id", state.UserID),
			attribute.String("slack.thread_ts", threadTS),
		),
	)
	defer span.End()

	values := callback.View.State.Values

	h.logger.InfoContext(ctx, "view submission received",
		"callback_id", callback.View.CallbackID,
		"user", state.UserID,
		"thread_ts", threadTS,
	)

	switch callback.View.CallbackID {
	case CallbackRepoStep1:
		if errs := validateRepoStep1(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		repoName := values[BlockName][ElemName].Value
		if msg := h.checkRepoAlreadyExistsCtx(ctx, repoName); msg != "" {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockName: msg},
			})
			return
		}
		state.RepoConfig.Name = repoName
		state.RepoConfig.Description = values[BlockDescription][ElemDescription].Value
		state.RepoConfig.Visibility = values[BlockVisibility][ElemVisibility].SelectedOption.Value
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		state.RepoConfig.HasIssues = true
		state.Phase = conversation.PhaseWizardStep1

		h.logger.InfoContext(ctx, "step1 parsed",
			"name", state.RepoConfig.Name,
			"description", state.RepoConfig.Description,
			"visibility", state.RepoConfig.Visibility,
		)

		state.AvailableTeams = h.fetchTeamNamesCtx(ctx)
		modal := RepoStep2Modal(state.AvailableTeams)
		modal.PrivateMetadata = threadTS + ":" + state.Nonce

		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		_ = responder.Ack(resp)

	case CallbackRepoStep2:
		if errs := validateRepoStep2(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		if topicsVal, ok := values[BlockTopics]; ok {
			raw := topicsVal[ElemTopics].Value
			if raw != "" {
				topics := strings.Split(raw, ",")
				for i, t := range topics {
					topics[i] = strings.TrimSpace(t)
				}
				state.RepoConfig.Topics = topics
			}
		}
		state.RepoConfig.TeamAccess = parseTeamRoleValues(values, state.AvailableTeams)
		state.RepoConfig.DefaultBranch = values[BlockDefBranch][ElemDefBranch].Value
		state.Phase = conversation.PhaseWizardStep2

		h.logger.InfoContext(ctx, "step2 parsed",
			"topics", state.RepoConfig.Topics,
			"team_access", state.RepoConfig.TeamAccess,
			"default_branch", state.RepoConfig.DefaultBranch,
		)

		modal := RepoStep3Modal()
		modal.PrivateMetadata = threadTS + ":" + state.Nonce
		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		_ = responder.Ack(resp)

	case CallbackRepoStep3:
		if errs := validateRepoStep3(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		h.parseStep3Values(state, values)
		_ = responder.Ack()

		rc := state.RepoConfig
		summary := RepoCreateSummary(
			rc.Name, rc.Description, rc.Visibility, rc.Topics, rc.TeamAccess,
			rc.DefaultBranch, rc.HasIssues, rc.EnableBranchProtection,
			rc.DismissStaleReviews, rc.RequireLinearHistory, rc.RequireConversationResolution,
			rc.RequiredReviews, rc.AllowAutoMerge, rc.AllowUpdateBranch, rc.DeleteBranchOnMerge,
			rc.HasDiscussions, rc.HasProjects, rc.HomepageURL,
			state.Justification,
		)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createPR(workflowContext(ctx), state)

	case CallbackDeleteRepo:
		targetRepo := values[BlockDeleteTarget][ElemDeleteTarget].SelectedOption.Value
		if msg := h.checkRepoStillExistsCtx(ctx, targetRepo); msg != "" {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockDeleteTarget: msg},
			})
			return
		}
		state.TargetRepo = targetRepo
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		_ = responder.Ack()

		summary := RepoDeleteSummary(state.TargetRepo, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDeletePR(workflowContext(ctx), state)

	case CallbackSelectRepo:
		repoName := values[BlockSelectRepo][ElemSelectRepo].SelectedOption.Value
		state.TargetRepo = repoName

		src, err := h.fetchHCLSourceCtx(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to fetch repos HCL for settings", "error", err)
			_ = responder.Ack()
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch the repository configuration.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		cfg, err := hcleditor.ExtractRepoConfig(src, repoName)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to extract repo config", "error", err, "repo", repoName)
			_ = responder.Ack()
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not read config for %s: %v", repoName, err), false))
			h.store.Delete(state.ThreadTS)
			return
		}
		state.RepoConfig = cfg
		state.Phase = conversation.PhaseActionSelected

		modal := SettingsStep1Modal(state.RepoConfig)
		modal.PrivateMetadata = threadTS + ":" + state.Nonce
		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		_ = responder.Ack(resp)

	case CallbackSettingsStep1:
		if errs := validateSettingsStep1(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		state.RepoConfig.Description = values[BlockDescription][ElemDescription].Value
		state.RepoConfig.Visibility = values[BlockVisibility][ElemVisibility].SelectedOption.Value
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		state.Phase = conversation.PhaseWizardStep1

		state.AvailableTeams = h.fetchTeamNamesCtx(ctx)
		modal := SettingsStep2Modal(state.RepoConfig, state.AvailableTeams)
		modal.PrivateMetadata = threadTS + ":" + state.Nonce
		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		_ = responder.Ack(resp)

	case CallbackSettingsStep2:
		if errs := validateSettingsStep2(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		if topicsVal, ok := values[BlockTopics]; ok {
			raw := topicsVal[ElemTopics].Value
			if raw != "" {
				topics := strings.Split(raw, ",")
				for i, t := range topics {
					topics[i] = strings.TrimSpace(t)
				}
				state.RepoConfig.Topics = topics
			} else {
				state.RepoConfig.Topics = nil
			}
		}
		state.RepoConfig.TeamAccess = parseTeamRoleValues(values, state.AvailableTeams)
		state.RepoConfig.DefaultBranch = values[BlockDefBranch][ElemDefBranch].Value
		state.Phase = conversation.PhaseWizardStep2

		modal := SettingsStep3Modal(state.RepoConfig)
		modal.PrivateMetadata = threadTS + ":" + state.Nonce
		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		_ = responder.Ack(resp)

	case CallbackSettingsStep3:
		if errs := validateRepoStep3(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		h.parseStep3Values(state, values)
		_ = responder.Ack()

		// fetch fresh HCL to get old config for comparison
		src, err := h.fetchHCLSourceCtx(ctx)
		if err != nil {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch current config for comparison.", false))
			h.store.Delete(state.ThreadTS)
			return
		}
		oldCfg, err := hcleditor.ExtractRepoConfig(src, state.TargetRepo)
		if err != nil {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not extract current settings for %s.", state.TargetRepo), false))
			h.store.Delete(state.ThreadTS)
			return
		}

		// check if anything changed
		if repoConfigEqual(oldCfg, state.RepoConfig) {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Nothing has changed. No PR needed.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		summary := RepoSettingsSummary(state.TargetRepo, oldCfg, state.RepoConfig, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createSettingsPR(workflowContext(ctx), state)

	// --- DNS flow callbacks ---

	case CallbackDnsAdd:
		if errs := validateDnsFields(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}

		newType := values[BlockDnsType][ElemDnsType].SelectedOption.Value
		newName := values[BlockDnsName][ElemDnsName].Value

		// check for DNS conflicts against live data
		if conflict := h.checkDnsAddConflictCtx(ctx, state.TargetZone, newName, newType); conflict != "" {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockDnsName: conflict},
			})
			return
		}

		state.DnsConfig.Type = newType
		state.DnsConfig.Name = newName
		state.DnsConfig.Content = values[BlockDnsContent][ElemDnsContent].Value
		if proxied, ok := values[BlockDnsProxied]; ok {
			state.DnsConfig.Proxied = len(proxied[ElemDnsProxied].SelectedOptions) > 0
		}
		if priority, ok := values[BlockDnsPriority]; ok {
			if n, err := strconv.Atoi(priority[ElemDnsPriority].Value); err == nil {
				state.DnsConfig.Priority = n
			}
		}
		if comment, ok := values[BlockDnsComment]; ok {
			state.DnsConfig.Comment = comment[ElemDnsComment].Value
		}
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		_ = responder.Ack()

		summary := DnsAddSummary(state.TargetZone, state.DnsConfig, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDnsAddPR(workflowContext(ctx), state)

	case CallbackDnsRemove:
		recordKey := values[BlockDnsRecord][ElemDnsRecord].SelectedOption.Value

		// verify the record still exists before proceeding
		if msg := h.checkDnsRecordStillExistsCtx(ctx, state.TargetZone, recordKey); msg != "" {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockDnsRecord: msg},
			})
			return
		}

		state.TargetRecord = recordKey
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		_ = responder.Ack()

		summary := DnsRemoveSummary(state.TargetZone, state.TargetRecord, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDnsRemovePR(workflowContext(ctx), state)

	case CallbackDnsSelectRecord:
		recordKey := values[BlockDnsRecord][ElemDnsRecord].SelectedOption.Value
		state.TargetRecord = recordKey

		src, err := h.fetchCloudflareHCLCtx(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to fetch DNS HCL for dns update", "error", err)
			_ = responder.Ack()
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch DNS configuration.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		cfg, err := hcleditor.ExtractDnsConfig(src, state.TargetZone, recordKey)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to extract dns config", "error", err, "record", recordKey)
			_ = responder.Ack()
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not read config for %s: %v", recordKey, err), false))
			h.store.Delete(state.ThreadTS)
			return
		}
		state.DnsConfig = cfg

		modal := DnsUpdateModal(state.TargetZone, state.DnsConfig)
		modal.PrivateMetadata = threadTS + ":" + state.Nonce
		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		_ = responder.Ack(resp)

	case CallbackDnsUpdate:
		if errs := validateDnsFields(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		state.DnsConfig.Type = values[BlockDnsType][ElemDnsType].SelectedOption.Value
		state.DnsConfig.Name = values[BlockDnsName][ElemDnsName].Value
		state.DnsConfig.Content = values[BlockDnsContent][ElemDnsContent].Value
		if proxied, ok := values[BlockDnsProxied]; ok {
			state.DnsConfig.Proxied = len(proxied[ElemDnsProxied].SelectedOptions) > 0
		} else {
			state.DnsConfig.Proxied = false
		}
		if priority, ok := values[BlockDnsPriority]; ok {
			if n, err := strconv.Atoi(priority[ElemDnsPriority].Value); err == nil {
				state.DnsConfig.Priority = n
			} else {
				state.DnsConfig.Priority = 0
			}
		} else {
			state.DnsConfig.Priority = 0
		}
		if comment, ok := values[BlockDnsComment]; ok {
			state.DnsConfig.Comment = comment[ElemDnsComment].Value
		} else {
			state.DnsConfig.Comment = ""
		}
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		_ = responder.Ack()

		// fetch fresh HCL for old config comparison
		src, err := h.fetchCloudflareHCLCtx(ctx)
		if err != nil {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch current DNS config for comparison.", false))
			h.store.Delete(state.ThreadTS)
			return
		}
		oldCfg, err := hcleditor.ExtractDnsConfig(src, state.TargetZone, state.TargetRecord)
		if err != nil {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not extract current config for %s.", state.TargetRecord), false))
			h.store.Delete(state.ThreadTS)
			return
		}

		if dnsConfigEqual(oldCfg, state.DnsConfig) {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Nothing has changed. No PR needed.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		summary := DnsUpdateSummary(state.TargetZone, oldCfg, state.DnsConfig, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDnsUpdatePR(workflowContext(ctx), state)

	// --- Org Settings flow callback ---

	case CallbackOrgSettings:
		if errs := validateOrgSettings(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}

		state.OrgConfig.Name = values[BlockOrgName][ElemOrgName].Value
		state.OrgConfig.BillingEmail = values[BlockOrgBilling][ElemOrgBilling].Value
		state.OrgConfig.Blog = values[BlockOrgBlog][ElemOrgBlog].Value
		state.OrgConfig.Description = values[BlockOrgDesc][ElemOrgDesc].Value
		state.OrgConfig.Location = values[BlockOrgLocation][ElemOrgLocation].Value
		state.OrgConfig.DefaultRepoPermission = values[BlockOrgPermission][ElemOrgPermission].SelectedOption.Value

		if mc, ok := values[BlockOrgMembersCreate]; ok {
			state.OrgConfig.MembersCanCreateRepos = len(mc[ElemOrgMembersCreate].SelectedOptions) > 0
		} else {
			state.OrgConfig.MembersCanCreateRepos = false
		}
		if so, ok := values[BlockOrgSignoff]; ok {
			state.OrgConfig.WebCommitSignoffRequired = len(so[ElemOrgSignoff].SelectedOptions) > 0
		} else {
			state.OrgConfig.WebCommitSignoffRequired = false
		}
		if da, ok := values[BlockOrgDepAlerts]; ok {
			state.OrgConfig.DependabotAlerts = len(da[ElemOrgDepAlerts].SelectedOptions) > 0
		} else {
			state.OrgConfig.DependabotAlerts = false
		}
		if ds, ok := values[BlockOrgDepSec]; ok {
			state.OrgConfig.DependabotSecurityUpdates = len(ds[ElemOrgDepSec].SelectedOptions) > 0
		} else {
			state.OrgConfig.DependabotSecurityUpdates = false
		}
		if dg, ok := values[BlockOrgDepGraph]; ok {
			state.OrgConfig.DependencyGraph = len(dg[ElemOrgDepGraph].SelectedOptions) > 0
		} else {
			state.OrgConfig.DependencyGraph = false
		}

		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		_ = responder.Ack()

		// fetch fresh HCL for comparison
		src, err := h.fetchOrgHCLSourceCtx(ctx)
		if err != nil {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch current config for comparison.", false))
			h.store.Delete(state.ThreadTS)
			return
		}
		oldCfg, err := hcleditor.ExtractOrgSettings(src)
		if err != nil {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Could not extract current org settings.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		if orgConfigEqual(oldCfg, state.OrgConfig) {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Nothing has changed. No PR needed.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		summary := OrgSettingsSummary(oldCfg, state.OrgConfig, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createOrgSettingsPR(workflowContext(ctx), state)

	// --- User Management flow callbacks ---

	case CallbackTeamMemberAdd:
		team := values[BlockTeamSelect][ElemTeamSelect].SelectedOption.Value
		username := values[BlockMemberSelect][ElemMemberSelect].SelectedOption.Value
		role := values[BlockRoleSelect][ElemRoleSelect].SelectedOption.Value
		if errMsg := validateTeamMemberAdd(team, username, role); errMsg != "" {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockTeamSelect: errMsg},
			})
			return
		}
		state.TeamMemberConfig = conversation.TeamMemberConfig{Team: team, Username: username, Role: role}
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		state.Phase = conversation.PhaseCreatingPR
		_ = responder.Ack()
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		go h.createTeamMemberPR(workflowContext(ctx), state)

	case CallbackTeamMemberRemove:
		team := values[BlockTeamSelect][ElemTeamSelect].SelectedOption.Value
		username := values[BlockMemberSelect][ElemMemberSelect].SelectedOption.Value
		if errMsg := validateTeamMemberRemove(team, username); errMsg != "" {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockTeamSelect: errMsg},
			})
			return
		}
		state.TeamMemberConfig = conversation.TeamMemberConfig{Team: team, Username: username}
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		state.Phase = conversation.PhaseCreatingPR
		_ = responder.Ack()
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		go h.createTeamMemberPR(workflowContext(ctx), state)

	case CallbackTeamMemberChangeRole:
		team := values[BlockTeamSelect][ElemTeamSelect].SelectedOption.Value
		username := values[BlockMemberSelect][ElemMemberSelect].SelectedOption.Value
		role := values[BlockRoleSelect][ElemRoleSelect].SelectedOption.Value
		if errMsg := validateTeamMemberChangeRole(team, username, role); errMsg != "" {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockTeamSelect: errMsg},
			})
			return
		}
		state.TeamMemberConfig = conversation.TeamMemberConfig{Team: team, Username: username, Role: role}
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		state.Phase = conversation.PhaseCreatingPR
		_ = responder.Ack()
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		go h.createTeamMemberPR(workflowContext(ctx), state)

	// --- Doppler Project flow callbacks ---

	case CallbackDopplerAdd:
		if errs := validateDopplerAdd(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		state.DopplerProjectConfig.Name = values[BlockDopplerName][ElemDopplerName].Value
		state.DopplerProjectConfig.Description = values[BlockDopplerDesc][ElemDopplerDesc].Value
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		_ = responder.Ack()

		summary := DopplerAddSummary(state.DopplerProjectConfig, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDopplerAddPR(workflowContext(ctx), state)

	case CallbackDopplerRemove:
		projectName := values[BlockDopplerSelect][ElemDopplerSelect].SelectedOption.Value

		if msg := h.checkProjectStillExistsCtx(ctx, projectName); msg != "" {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockDopplerSelect: msg},
			})
			return
		}

		state.TargetProject = projectName
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		_ = responder.Ack()

		summary := DopplerRemoveSummary(state.TargetProject, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDopplerRemovePR(workflowContext(ctx), state)

	case CallbackDopplerSelectProject:
		projectName := values[BlockDopplerSelect][ElemDopplerSelect].SelectedOption.Value
		state.TargetProject = projectName

		src, err := h.fetchDopplerHCLCtx(ctx)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to fetch Doppler HCL for project update", "error", err)
			_ = responder.Ack()
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch Doppler configuration.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		cfg, err := hcleditor.ExtractProjectConfig(src, projectName)
		if err != nil {
			h.logger.ErrorContext(ctx, "failed to extract project config", "error", err, "project", projectName)
			_ = responder.Ack()
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not read config for %s: %v", projectName, err), false))
			h.store.Delete(state.ThreadTS)
			return
		}
		state.DopplerProjectConfig = cfg

		modal := DopplerProjectUpdateModal(projectName, cfg.Description)
		modal.PrivateMetadata = threadTS + ":" + state.Nonce
		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		_ = responder.Ack(resp)

	case CallbackDopplerUpdate:
		if errs := validateDopplerUpdate(values); len(errs) > 0 {
			_ = responder.Ack(map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		newDesc := values[BlockDopplerDesc][ElemDopplerDesc].Value
		oldDesc := state.DopplerProjectConfig.Description
		state.DopplerProjectConfig.Description = newDesc
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		_ = responder.Ack()

		if oldDesc == newDesc {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Nothing has changed. No PR needed.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		summary := DopplerUpdateSummary(state.TargetProject, oldDesc, newDesc, state.Justification)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDopplerUpdatePR(workflowContext(ctx), state)

	default:
		_ = responder.Ack()
	}
}

func (h *Handler) parseStep3Values(state *conversation.State, values map[string]map[string]slack.BlockAction) {
	rc := &state.RepoConfig

	if prot, ok := values[BlockProtection]; ok {
		rc.EnableBranchProtection = len(prot[ElemProtection].SelectedOptions) > 0
	}
	if reviews, ok := values[BlockReviews]; ok {
		if n, err := strconv.Atoi(reviews[ElemReviews].Value); err == nil {
			rc.RequiredReviews = n
		}
	}
	if dismiss, ok := values[BlockDismissStale]; ok {
		rc.DismissStaleReviews = len(dismiss[ElemDismissStale].SelectedOptions) > 0
	}
	if linear, ok := values[BlockLinear]; ok {
		rc.RequireLinearHistory = len(linear[ElemLinear].SelectedOptions) > 0
	}
	if conv, ok := values[BlockConvRes]; ok {
		rc.RequireConversationResolution = len(conv[ElemConvRes].SelectedOptions) > 0
	}
	if am, ok := values[BlockAutoMerge]; ok {
		rc.AllowAutoMerge = len(am[ElemAutoMerge].SelectedOptions) > 0
	}
	if ub, ok := values[BlockUpdateBranch]; ok {
		rc.AllowUpdateBranch = len(ub[ElemUpdateBranch].SelectedOptions) > 0
	}
	if db, ok := values[BlockDeleteBranch]; ok {
		rc.DeleteBranchOnMerge = len(db[ElemDeleteBranch].SelectedOptions) > 0
	}
	if disc, ok := values[BlockDiscussions]; ok {
		rc.HasDiscussions = len(disc[ElemDiscussions].SelectedOptions) > 0
	}
	if proj, ok := values[BlockProjects]; ok {
		rc.HasProjects = len(proj[ElemProjects].SelectedOptions) > 0
	}
	if hp, ok := values[BlockHomepage]; ok {
		rc.HomepageURL = strings.TrimSpace(hp[ElemHomepage].Value)
	}
}

func writeBulletResourceDetails(sb *strings.Builder, state *conversation.State) {
	switch state.ResourceType {
	case "repo":
		switch state.ActionType {
		case "add":
			writeBuilderf(sb, "• *Repository:* %s\n", state.RepoConfig.Name)
			if state.RepoConfig.Description != "" {
				writeBuilderf(sb, "• *Description:* %s\n", state.RepoConfig.Description)
			}
			writeBuilderf(sb, "• *Visibility:* %s\n", state.RepoConfig.Visibility)
		case "delete", "settings":
			writeBuilderf(sb, "• *Repository:* %s\n", state.TargetRepo)
		}
	case "dns":
		switch state.ActionType {
		case "add":
			writeBuilderf(sb, "• *Zone:* %s\n", state.TargetZone)
			writeBuilderf(sb, "• *Record:* %s (%s) -> %s\n", state.DnsConfig.Name, state.DnsConfig.Type, state.DnsConfig.Content)
		case "delete", "settings":
			writeBuilderf(sb, "• *Zone:* %s\n", state.TargetZone)
			writeBuilderf(sb, "• *Record:* %s\n", state.TargetRecord)
		}
	case "org_settings":
		sb.WriteString("• *Resource:* Organization settings\n")
	case "user_management":
		writeBuilderf(sb, "• *Team:* %s\n", state.TeamMemberConfig.Team)
		writeBuilderf(sb, "• *Member:* %s\n", state.TeamMemberConfig.Username)
		if state.ActionType != "delete" {
			writeBuilderf(sb, "• *Role:* %s\n", state.TeamMemberConfig.Role)
		}
	}
}

func buildRequestSummary(state *conversation.State, prTitle, prURL string) []slack.Block {
	var sb strings.Builder
	now := time.Now().UTC().Format("2 Jan 2006, 15:04 UTC")
	writeBuilderf(&sb, "• *Request:* %s\n", requestSummaryTitle(prTitle))
	writeBuilderf(&sb, "• *Requested by:* <@%s>\n", state.UserID)
	writeBuilderf(&sb, "• *Requested at:* %s\n", now)
	writeBuilderf(&sb, "• *Priority:* %s %s\n", priorityEmoji(state.Priority), capitalizeFirst(state.Priority))

	writeBulletResourceDetails(&sb, state)

	if state.Justification != "" {
		writeBuilderf(&sb, "• *Justification:* %s\n", state.Justification)
	}

	prLabel := "View PR"
	if matches := prURLPattern.FindStringSubmatch(prURL); len(matches) >= 2 {
		prLabel = "#" + matches[1]
	}
	writeBuilderf(&sb, "• *PR:* <%s|%s>\n", prURL, prLabel)

	section := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", sb.String(), false, false),
		nil, nil,
	)
	return []slack.Block{section}
}

func requestSummaryTitle(prTitle string) string {
	return strings.TrimSpace(strings.TrimPrefix(prTitle, "Request:"))
}

func capitalizeFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (h *Handler) fetchTeamNamesCtx(ctx context.Context) []string {
	src, _, err := h.gh.GetFileContent(ctx, pathGitHubMembers)
	if err != nil {
		h.logger.Error("failed to fetch members HCL for teams", "error", err)
		return []string{"Maintainers"}
	}
	teams, err := hcleditor.ExtractTeamNames(src)
	if err != nil {
		h.logger.Error("failed to extract teams", "error", err)
		return []string{"Maintainers"}
	}
	return teams
}

func (h *Handler) fetchMemberNamesCtx(ctx context.Context) []string {
	src, _, err := h.gh.GetFileContent(ctx, pathGitHubMembers)
	if err != nil {
		h.logger.Error("failed to fetch members HCL for member names", "error", err)
		return nil
	}
	names, err := hcleditor.ExtractMemberNames(src)
	if err != nil {
		h.logger.Error("failed to extract member names", "error", err)
		return nil
	}
	return names
}

func (h *Handler) handleUserManagementAction(ctx context.Context, state *conversation.State, actionType, triggerID string) {
	teams := h.fetchTeamNamesCtx(ctx)
	if len(teams) == 0 {
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No teams found.", false))
		h.store.Delete(state.ThreadTS)
		return
	}
	members := h.fetchMemberNamesCtx(ctx)
	if len(members) == 0 {
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No org members found.", false))
		h.store.Delete(state.ThreadTS)
		return
	}
	state.AvailableTeams = teams
	state.AvailableMembers = members

	meta := state.ThreadTS + ":" + state.Nonce

	var modal slack.ModalViewRequest
	switch actionType {
	case "add":
		modal = TeamMemberAddModal(teams, members, meta)
	case "delete":
		modal = TeamMemberRemoveModal(teams, members, meta)
	case "change_role":
		modal = TeamMemberChangeRoleModal(teams, members, meta)
	default:
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionBlocks(ComingSoonBlocks(actionType)...))
		h.store.Delete(state.ThreadTS)
		return
	}

	if err := h.openView(ctx, triggerID, modal); err != nil {
		h.logger.Error("failed to open team member modal", "error", err, "action", actionType)
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
	}
}

func (h *Handler) createTeamMemberPR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	cfg := state.TeamMemberConfig

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubMembers)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch members HCL", err)
		return
	}

	var modified []byte
	switch state.ActionType {
	case "add":
		modified, err = hcleditor.AddTeamMember(src, cfg.Team, cfg.Username, cfg.Role)
	case "delete":
		modified, err = hcleditor.RemoveTeamMember(src, cfg.Team, cfg.Username)
	case "change_role":
		modified, err = hcleditor.UpdateTeamMemberRole(src, cfg.Team, cfg.Username, cfg.Role)
	default:
		workflowErr = fmt.Errorf("unsupported action: %s", state.ActionType)
		h.reportError(ctx, state, "unknown action", workflowErr)
		return
	}
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.MemberBranchName(state.ActionType, cfg.Team, cfg.Username)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	var commitVerb string
	switch state.ActionType {
	case "add":
		commitVerb = "add"
	case "delete":
		commitVerb = "remove"
	case "change_role":
		commitVerb = "update role for"
	}
	commitMsg := fmt.Sprintf("feat(github): %s %s in team %s", commitVerb, cfg.Username, cfg.Team)
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubMembers, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	var prTitle string
	switch state.ActionType {
	case "add":
		prTitle = fmt.Sprintf("Request: Add %s to team %s", cfg.Username, cfg.Team)
	case "delete":
		prTitle = fmt.Sprintf("Request: Remove %s from team %s", cfg.Username, cfg.Team)
	case "change_role":
		prTitle = fmt.Sprintf("Request: Change %s role in team %s", cfg.Username, cfg.Team)
	}
	prBody := ghclient.BuildMemberPRDescription(state.ActionType, cfg.Team, cfg.Username, cfg.Role, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func (h *Handler) createPR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	repo := state.RepoConfig

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch repos HCL", err)
		return
	}

	modified, err := hcleditor.AddRepo(src, repo)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.BranchName(repo.Name)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(github): add repository %s", repo.Name)
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubRepos, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Add GitHub repository"
	prBody := ghclient.BuildPRDescription(repo.Name, repo.Description, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func (h *Handler) createDeletePR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	repoName := state.TargetRepo

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch repos HCL", err)
		return
	}

	modified, err := hcleditor.RemoveRepo(src, repoName)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.DeleteBranchName(repoName)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(github): remove repository %s", repoName)
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubRepos, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Remove GitHub repository"
	prBody := ghclient.BuildDeletePRDescription(repoName, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func (h *Handler) fetchRepoNamesCtx(ctx context.Context) []string {
	src, _, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	if err != nil {
		h.logger.Error("failed to fetch repos HCL", "error", err)
		return nil
	}
	names, err := hcleditor.ExistingRepoNames(src)
	if err != nil {
		h.logger.Error("failed to extract repo names", "error", err)
		return nil
	}
	return names
}

func (h *Handler) checkRepoAlreadyExistsCtx(ctx context.Context, name string) string {
	names := h.fetchRepoNamesCtx(ctx)
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return fmt.Sprintf("Repository %q already exists.", n)
		}
	}
	return ""
}

func (h *Handler) checkRepoStillExistsCtx(ctx context.Context, name string) string {
	names := h.fetchRepoNamesCtx(ctx)
	for _, n := range names {
		if n == name {
			return ""
		}
	}
	return fmt.Sprintf("Repository %q no longer exists. It may have been removed already.", name)
}

func (h *Handler) fetchHCLSourceCtx(ctx context.Context) ([]byte, error) {
	src, _, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	return src, err
}

func (h *Handler) fetchOrgHCLSourceCtx(ctx context.Context) ([]byte, error) {
	src, _, err := h.gh.GetFileContent(ctx, pathGitHubOrg)
	return src, err
}

func (h *Handler) createSettingsPR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	repoName := state.TargetRepo

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch repos HCL", err)
		return
	}

	modified, err := hcleditor.UpdateRepo(src, repoName, state.RepoConfig)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.SettingsBranchName(repoName)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(github): update repository settings for %s", repoName)
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubRepos, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Update GitHub repository settings"
	prBody := ghclient.BuildSettingsPRDescription(repoName, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

// --- DNS PR creation ---

func (h *Handler) createDnsAddPR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	zone := state.TargetZone
	cfg := state.DnsConfig

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathCloudflareDNS)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch DNS HCL", err)
		return
	}

	existingKeys, err := hcleditor.ExistingDnsRecordKeys(src, zone)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "read existing DNS keys", err)
		return
	}
	cfg.RecordKey = generateDnsRecordKey(cfg.Name, cfg.Type, existingKeys)

	modified, err := hcleditor.AddDnsRecord(src, zone, cfg)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.DnsBranchName("add", cfg.RecordKey)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(cloudflare): add DNS record %s", cfg.RecordKey)
	if err := h.gh.UpdateFile(ctx, branch, pathCloudflareDNS, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Add DNS record"
	prBody := ghclient.BuildDnsPRDescription("add", zone, cfg.RecordKey, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func (h *Handler) createDnsRemovePR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	zone := state.TargetZone
	recordKey := state.TargetRecord

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathCloudflareDNS)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch DNS HCL", err)
		return
	}

	modified, err := hcleditor.RemoveDnsRecord(src, zone, recordKey)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.DnsBranchName("delete", recordKey)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(cloudflare): remove DNS record %s", recordKey)
	if err := h.gh.UpdateFile(ctx, branch, pathCloudflareDNS, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Remove DNS record"
	prBody := ghclient.BuildDnsPRDescription("delete", zone, recordKey, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func (h *Handler) createDnsUpdatePR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	zone := state.TargetZone
	recordKey := state.TargetRecord

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathCloudflareDNS)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch DNS HCL", err)
		return
	}

	modified, err := hcleditor.UpdateDnsRecord(src, zone, recordKey, state.DnsConfig)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.DnsBranchName("update", recordKey)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(cloudflare): update DNS record %s", recordKey)
	if err := h.gh.UpdateFile(ctx, branch, pathCloudflareDNS, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Update DNS record"
	prBody := ghclient.BuildDnsPRDescription("settings", zone, recordKey, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func (h *Handler) createOrgSettingsPR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubOrg)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch org HCL", err)
		return
	}

	modified, err := hcleditor.UpdateOrgSettings(src, state.OrgConfig)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.OrgSettingsBranchName()
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := "feat(github): update organization settings"
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubOrg, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Update GitHub organization settings"
	prBody := ghclient.BuildOrgSettingsPRDescription(requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func orgConfigEqual(a, b conversation.OrgConfig) bool {
	return a == b
}

func (h *Handler) fetchCloudflareHCLCtx(ctx context.Context) ([]byte, error) {
	src, _, err := h.gh.GetFileContent(ctx, pathCloudflareDNS)
	return src, err
}

func (h *Handler) fetchDnsRecordKeys(src []byte, zone string) []string {
	keys, err := hcleditor.ExistingDnsRecordKeys(src, zone)
	if err != nil {
		h.logger.Error("failed to extract dns record keys", "error", err)
		return nil
	}
	return keys
}

func (h *Handler) fetchDnsRecordOptions(src []byte, zone string) []DnsRecordOption {
	keys := h.fetchDnsRecordKeys(src, zone)
	opts := make([]DnsRecordOption, 0, len(keys))
	for _, k := range keys {
		cfg, err := hcleditor.ExtractDnsConfig(src, zone, k)
		if err != nil {
			h.logger.Error("failed to extract dns config for option", "error", err, "key", k)
			opts = append(opts, DnsRecordOption{Key: k, Label: k})
			continue
		}
		content := cfg.Content
		if len(content) > 30 {
			content = content[:30] + "..."
		}
		opts = append(opts, DnsRecordOption{
			Key:   k,
			Label: fmt.Sprintf("%s (%s) %s", cfg.Name, cfg.Type, content),
		})
	}
	return opts
}

func (h *Handler) checkDnsAddConflictCtx(ctx context.Context, zone, name, typ string) string {
	src, err := h.fetchCloudflareHCLCtx(ctx)
	if err != nil {
		h.logger.Error("failed to fetch HCL for conflict check", "error", err)
		return ""
	}
	keys, err := hcleditor.ExistingDnsRecordKeys(src, zone)
	if err != nil {
		h.logger.Error("failed to read existing dns keys for conflict check", "error", err)
		return ""
	}
	var existing []conversation.DnsConfig
	for _, k := range keys {
		cfg, err := hcleditor.ExtractDnsConfig(src, zone, k)
		if err != nil {
			continue
		}
		existing = append(existing, cfg)
	}
	return checkDnsConflict(name, typ, existing)
}

func (h *Handler) checkDnsRecordStillExistsCtx(ctx context.Context, zone, key string) string {
	src, err := h.fetchCloudflareHCLCtx(ctx)
	if err != nil {
		h.logger.Error("failed to fetch HCL for record existence check", "error", err)
		return ""
	}
	keys, err := hcleditor.ExistingDnsRecordKeys(src, zone)
	if err != nil {
		h.logger.Error("failed to read existing dns keys for existence check", "error", err)
		return ""
	}
	return checkDnsRecordExists(key, keys)
}

func (h *Handler) resolveRequester(ctx context.Context, state *conversation.State) string {
	requester := state.UserID
	if user, err := h.getUserInfo(ctx, state.UserID); err != nil {
		h.logger.ErrorContext(ctx, "failed to resolve slack user name", "error", err, "user_id", state.UserID)
	} else {
		requester = user.RealName
	}
	return requester
}

func dnsConfigEqual(a, b conversation.DnsConfig) bool {
	return a.Type == b.Type &&
		a.Name == b.Name &&
		a.Content == b.Content &&
		a.Proxied == b.Proxied &&
		a.Priority == b.Priority &&
		a.Comment == b.Comment
}

// repoConfigEqual compares two RepoConfig values for equality.
func repoConfigEqual(a, b conversation.RepoConfig) bool {
	if a.Description != b.Description || a.Visibility != b.Visibility || a.DefaultBranch != b.DefaultBranch {
		return false
	}
	if a.HasIssues != b.HasIssues || a.HasWiki != b.HasWiki {
		return false
	}
	if a.HasDiscussions != b.HasDiscussions || a.HasProjects != b.HasProjects {
		return false
	}
	if a.HomepageURL != b.HomepageURL {
		return false
	}
	if a.AllowAutoMerge != b.AllowAutoMerge || a.AllowUpdateBranch != b.AllowUpdateBranch || a.DeleteBranchOnMerge != b.DeleteBranchOnMerge {
		return false
	}
	if a.EnableBranchProtection != b.EnableBranchProtection {
		return false
	}
	if a.EnableBranchProtection && b.EnableBranchProtection {
		if a.RequiredReviews != b.RequiredReviews || a.DismissStaleReviews != b.DismissStaleReviews {
			return false
		}
		if a.RequireLinearHistory != b.RequireLinearHistory || a.RequireConversationResolution != b.RequireConversationResolution {
			return false
		}
	}
	if len(a.Topics) != len(b.Topics) {
		return false
	}
	for i := range a.Topics {
		if a.Topics[i] != b.Topics[i] {
			return false
		}
	}
	if len(a.TeamAccess) != len(b.TeamAccess) {
		return false
	}
	for k, v := range a.TeamAccess {
		if b.TeamAccess[k] != v {
			return false
		}
	}
	return true
}

func (h *Handler) reportError(ctx context.Context, state *conversation.State, step string, err error) {
	trace.SpanFromContext(ctx).RecordError(err)
	captureWorkflowError(ctx, state, step, err)
	h.logger.ErrorContext(ctx, "PR creation failed", "step", step, "error", err, "user", state.UserID)
	h.postMessageCtx(ctx, state.ChannelID, "report PR creation failure",
		slack.MsgOptionText(fmt.Sprintf("Something went wrong at %s: %v", step, err), false),
		slack.MsgOptionTS(state.ThreadTS))

	current := h.store.Get(state.ThreadTS)
	if current != nil && current.Nonce == state.Nonce {
		h.store.Delete(state.ThreadTS)
	}
}

func (h *Handler) handleDopplerAction(ctx context.Context, state *conversation.State, actionType, triggerID string) {
	switch actionType {
	case "add":
		modal := DopplerProjectAddModal()
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		if err := h.openView(ctx, triggerID, modal); err != nil {
			h.logger.Error("failed to open doppler add modal", "error", err)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "delete":
		projects := h.fetchProjectNamesCtx(ctx)
		if len(projects) == 0 {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No Doppler projects found. There's nothing to remove.", false))
			return
		}
		modal := DopplerProjectRemoveModal(projects)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		if err := h.openView(ctx, triggerID, modal); err != nil {
			h.logger.Error("failed to open doppler remove modal", "error", err)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "settings":
		projects := h.fetchProjectNamesCtx(ctx)
		if len(projects) == 0 {
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("No Doppler projects found. There's nothing to update.", false))
			return
		}
		modal := DopplerProjectSelectModal(projects)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		if err := h.openView(ctx, triggerID, modal); err != nil {
			h.logger.Error("failed to open doppler select project modal", "error", err)
			h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	}
}

func (h *Handler) fetchDopplerHCLCtx(ctx context.Context) ([]byte, error) {
	src, _, err := h.gh.GetFileContent(ctx, pathDopplerProjects)
	return src, err
}

func (h *Handler) fetchProjectNamesCtx(ctx context.Context) []string {
	src, err := h.fetchDopplerHCLCtx(ctx)
	if err != nil {
		h.logger.Error("failed to fetch Doppler HCL for project names", "error", err)
		return nil
	}
	names, err := hcleditor.ExistingProjectNames(src)
	if err != nil {
		h.logger.Error("failed to extract project names", "error", err)
		return nil
	}
	return names
}

func (h *Handler) checkProjectStillExistsCtx(ctx context.Context, projectName string) string {
	src, err := h.fetchDopplerHCLCtx(ctx)
	if err != nil {
		h.logger.Error("failed to fetch HCL for project existence check", "error", err)
		return ""
	}
	names, err := hcleditor.ExistingProjectNames(src)
	if err != nil {
		h.logger.Error("failed to read existing project names for existence check", "error", err)
		return ""
	}
	for _, n := range names {
		if n == projectName {
			return ""
		}
	}
	return fmt.Sprintf("Project %q no longer exists.", projectName)
}

func (h *Handler) createDopplerAddPR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	cfg := state.DopplerProjectConfig

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathDopplerProjects)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch Doppler HCL", err)
		return
	}

	modified, err := hcleditor.AddProject(src, cfg)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.DopplerBranchName("add", cfg.Name)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(doppler): add project %s", cfg.Name)
	if err := h.gh.UpdateFile(ctx, branch, pathDopplerProjects, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Add Doppler project"
	prBody := ghclient.BuildDopplerPRDescription("add", cfg.Name, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func (h *Handler) createDopplerRemovePR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	projectName := state.TargetProject

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathDopplerProjects)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch Doppler HCL", err)
		return
	}

	modified, err := hcleditor.RemoveProject(src, projectName)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.DopplerBranchName("delete", projectName)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(doppler): remove project %s", projectName)
	if err := h.gh.UpdateFile(ctx, branch, pathDopplerProjects, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Remove Doppler project"
	prBody := ghclient.BuildDopplerPRDescription("delete", projectName, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

func (h *Handler) createDopplerUpdatePR(parent context.Context, state *conversation.State) {
	workflowName := workflowNameForState(state)
	attrs := h.workflowAttrs(workflowName, state)
	ctx, span, started := h.startWorkflow(parent, workflowName, attrs...)
	var workflowErr error
	defer func() {
		h.finishWorkflow(ctx, span, started, workflowErr, attrs...)
	}()

	projectName := state.TargetProject
	cfg := state.DopplerProjectConfig

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathDopplerProjects)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "fetch Doppler HCL", err)
		return
	}

	modified, err := hcleditor.UpdateProject(src, projectName, cfg)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "modify HCL", err)
		return
	}

	branch := ghclient.DopplerBranchName("settings", projectName)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(doppler): update project %s", projectName)
	if err := h.gh.UpdateFile(ctx, branch, pathDopplerProjects, modified, fileSHA, commitMsg); err != nil {
		workflowErr = err
		h.reportError(ctx, state, "commit file", err)
		return
	}

	requester := h.resolveRequester(ctx, state)
	prTitle := "Request: Update Doppler project"
	prBody := ghclient.BuildDopplerPRDescription("settings", projectName, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		workflowErr = err
		h.reportError(ctx, state, "create PR", err)
		return
	}

	h.replyPRCtx(ctx, state, prTitle, prURL)
}

// lockFlowMessages updates all tracked interactive messages to static locked versions.
// Runs synchronously -- callers that need non-blocking behavior should call in a goroutine.
func (h *Handler) lockFlowMessages(state *conversation.State) {
	for _, msg := range state.Messages {
		var blocks []slack.Block
		switch msg.Kind {
		case conversation.MsgCategory:
			blocks = LockedCategoryBlocks(msg.Label)
		case conversation.MsgResource:
			blocks = LockedResourceBlocks(state.Category, msg.Label)
		case conversation.MsgAction:
			blocks = LockedActionBlocks(msg.Label)
		case conversation.MsgConfirmation:
			blocks = LockedConfirmationBlocks()
		case conversation.MsgWelcome:
			blocks = FlowEndedBlocks()
		default:
			continue
		}
		h.updateChannelMessage(state.ChannelID, msg.TS, "lock flow message", slack.MsgOptionBlocks(blocks...))
	}
}
