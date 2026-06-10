// Package slack hosts the conCierge Slack integration: it dispatches Events
// API and interactivity callbacks, drives the dynamic schema-backed wizard,
// and orchestrates Terraform PR creation. Sub-files split concerns:
//
//	events.go       - inbound Slack events (DMs, App Home, assistant threads)
//	interactive.go  - block-action handlers and dynamic modal launch
//	submission.go   - view submission parsing and dispatch
//	pr.go           - Terraform mutation + PR creation orchestration
//	summary.go      - reply helpers, request summaries, flow message locking
//	schema.go       - runtime schema fetch + cache
//	ids.go          - block/element/callback ID constants and parsing
package slack

import (
	"context"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/jae-labs/concierge/internal/conversation"
	ghclient "github.com/jae-labs/concierge/internal/github"
	"github.com/jae-labs/concierge/internal/schema"
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
	// pathConciergeSchema is the only fixed Terraform path; every other file
	// path comes from the parsed schema's resource.File entries.
	pathConciergeSchema = "concierge-schema.yaml"
)

// prURLPattern extracts the PR number from a github.com PR HTML URL.
var prURLPattern = regexp.MustCompile(`/pull/(\d+)`)

// Handler is the long-lived Slack integration. One instance per process.
type Handler struct {
	api                *slack.Client
	sm                 *socketmode.Client
	store              *conversation.Store
	gh                 *ghclient.Client
	schemaReader       schemaFileReader
	eventsAPIProcessor func(slackevents.EventsAPIEvent) // test seam
	logger             *slog.Logger
	requestsChannelID  string
	userIDs            map[string]bool

	// Observability instruments are noop-safe when OTel is not configured.
	tracer           trace.Tracer
	eventsTotal      metric.Int64Counter
	prCreated        metric.Int64Counter
	slackAPITotal    metric.Int64Counter
	slackAPIDuration metric.Float64Histogram
	workflowTotal    metric.Int64Counter
	workflowDuration metric.Float64Histogram

	schemaMu      sync.RWMutex
	runtimeSchema *schema.Schema
}

// schemaFileReader is the subset of *github.Client used to fetch the schema
// file. Defined as interface for testability.
type schemaFileReader interface {
	GetFileContent(ctx context.Context, path string) ([]byte, string, error)
}

// interactionResponder abstracts how interactivity acks reach Slack so the
// HTTP and Socket Mode entry points can share view-submission code.
type interactionResponder interface {
	Ack(payload ...any) error
}

type interactionResponderFunc func(payload ...any) error

func (f interactionResponderFunc) Ack(payload ...any) error { return f(payload...) }

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
	var reader schemaFileReader
	if gh != nil {
		reader = gh
	}
	return &Handler{
		api:               api,
		store:             conversation.NewStore(),
		gh:                gh,
		schemaReader:      reader,
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

// isAuthorized is the sole authorization gate; gated by SLACK_USER_IDS.
func (h *Handler) isAuthorized(userID string) bool { return h.userIDs[userID] }

// RunSocketMode starts a background event loop and runs the Socket Mode
// client until ctx is cancelled.
func (h *Handler) RunSocketMode(ctx context.Context, sm *socketmode.Client) error {
	h.sm = sm
	go h.eventLoop()
	return h.sm.RunContext(ctx)
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

func (h *Handler) ackRequest(req *socketmode.Request, payload ...any) {
	if h.sm == nil || req == nil {
		return
	}
	if err := h.sm.Ack(*req, payload...); err != nil {
		h.logger.ErrorContext(context.Background(), "failed to acknowledge socket mode request", "error", err)
	}
}

// replyCtx posts a threaded reply and tracks it on state for later locking.
func (h *Handler) replyCtx(ctx context.Context, state *conversation.State, kind conversation.MessageKind, label string, opts ...slack.MsgOption) string {
	opts = append(opts, slack.MsgOptionTS(state.ThreadTS))
	ts := h.postMessageCtx(ctx, state.ChannelID, "reply", opts...)
	if ts != "" {
		state.TrackMessage(ts, kind, label)
	}
	return ts
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
