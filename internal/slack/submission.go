package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jae-labs/concierge/internal/conversation"
	hcleditor "github.com/jae-labs/concierge/internal/hcl"
	"github.com/jae-labs/concierge/internal/schema"
	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const slowAckThreshold = 2500 * time.Millisecond

// handleViewSubmission validates the envelope and dispatches to the
// per-callback submission handlers.
func (h *Handler) handleViewSubmission(parent context.Context, callback slack.InteractionCallback, responder interactionResponder) {
	state, threadTS, ok := h.resolveSubmissionState(parent, callback, responder)
	if !ok {
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

	h.logger.InfoContext(ctx, "view submission received",
		"callback_id", callback.View.CallbackID,
		"user", state.UserID,
		"thread_ts", threadTS,
	)

	ack := newViewAcker(ctx, h.logger, responder, span, state, callback.View.CallbackID)

	switch callback.View.CallbackID {
	case CallbackDynamicSelectTarget:
		h.submitSelectTarget(ctx, callback, state, ack)
	default:
		if step, ok := parseDynamicCallback(callback.View.CallbackID); ok {
			h.submitDynamicStep(ctx, callback, state, step, ack)
			return
		}
		_ = ack.send()
	}
}

// resolveSubmissionState validates PrivateMetadata, nonce, user identity, and
// authorization. Returns (nil, "", false) when the submission must be silently
// dropped after an empty ack.
func (h *Handler) resolveSubmissionState(ctx context.Context, callback slack.InteractionCallback, responder interactionResponder) (*conversation.State, string, bool) {
	parts := strings.SplitN(callback.View.PrivateMetadata, ":", 2)
	if len(parts) != 2 {
		h.logger.WarnContext(ctx, "invalid PrivateMetadata format", "metadata", callback.View.PrivateMetadata)
		_ = responder.Ack()
		return nil, "", false
	}
	threadTS, nonce := parts[0], parts[1]

	state := h.store.Get(threadTS)
	if state == nil || state.Nonce != nonce {
		h.logger.WarnContext(ctx, "no flow or nonce mismatch for modal submission", "thread_ts", threadTS)
		_ = responder.Ack()
		return nil, "", false
	}
	if callback.User.ID != state.UserID {
		h.logger.WarnContext(ctx, "unauthorized modal submission: user mismatch",
			"expected", state.UserID, "actual", callback.User.ID)
		_ = responder.Ack()
		return nil, "", false
	}
	if !h.isAuthorized(callback.User.ID) {
		h.logger.WarnContext(ctx, "unauthorized modal submission: user not authorized", "user", callback.User.ID)
		_ = responder.Ack()
		return nil, "", false
	}
	return state, threadTS, true
}

// submitSelectTarget handles the dynamic_select_target callback for map-entry
// update/delete flows. On delete it jumps straight to PR creation; on update
// it loads the existing values and opens the first edit step.
func (h *Handler) submitSelectTarget(ctx context.Context, callback slack.InteractionCallback, state *conversation.State, ack *viewAcker) {
	resource, err := h.fetchSchema(ctx, state.ResourceType)
	if err != nil {
		_ = ack.errorMsg("Failed to load schema.")
		return
	}
	if resource.Kind != schema.KindMapEntry {
		_ = ack.send()
		return
	}

	values := callback.View.State.Values
	state.TargetRepo = values[BlockResourceKey][ElemResourceKey].SelectedOption.Value
	state.Justification = values[BlockJustification][ElemJustification].Value

	if state.ActionType == schema.ActionDelete {
		_ = ack.send()
		h.queuePR(ctx, state, resource)
		return
	}

	src, err := h.dynamicFileSource(ctx, state, resource)
	if err != nil {
		_ = ack.errorMsg("Failed to fetch file content.")
		return
	}
	existingValues, err := hcleditor.ReadResource(src, resource.RootPath, state.TargetRepo, resource)
	if err != nil {
		_ = ack.errorMsg("Failed to read resource values.")
		return
	}
	state.DynamicConfig = existingValues

	dynamicKeys := cachedDynamicKeys(state, func() map[string][]string {
		return h.fetchDynamicKeys(ctx, resource)
	})
	metadata := state.ThreadTS + ":" + state.Nonce
	modal := BuildDynamicModal(resource, 0, existingValues, dynamicKeys,
		dynamicCallback{Mode: flowUpdate, Step: 1}.String(), metadata)
	_ = ack.update(modal)
}

func (h *Handler) dynamicFileSource(ctx context.Context, state *conversation.State, resource *schema.Resource) ([]byte, error) {
	if len(state.DynamicFileContent) > 0 {
		return state.DynamicFileContent, nil
	}
	src, _, err := h.gh.GetFileContent(ctx, resource.File)
	return src, err
}

// submitDynamicStep handles a dynamic_(create|update)_step_N submission:
// validates, merges values, then either advances to the next step or queues
// the PR creation goroutine.
func (h *Handler) submitDynamicStep(ctx context.Context, callback slack.InteractionCallback, state *conversation.State, step dynamicCallback, ack *viewAcker) {
	resource, err := h.fetchSchema(ctx, state.ResourceType)
	if err != nil {
		_ = ack.errorMsg("Failed to load schema.")
		return
	}
	stepIdx := step.Step - 1
	if stepIdx < 0 || stepIdx >= len(resource.Steps) {
		_ = ack.send()
		return
	}

	values := callback.View.State.Values
	dynamicKeys := cachedDynamicKeys(state, func() map[string][]string {
		return h.fetchDynamicKeys(ctx, resource)
	})
	errs := ValidateDynamicSubmission(values, resource.Steps[stepIdx], dynamicKeys)

	isCreate := step.Mode == flowCreate
	if isCreateKeyStep(resource, isCreate, stepIdx) {
		validateCreateKey(resource, values, state.DynamicResourceKeys, errs)
	}
	if len(errs) > 0 {
		_ = ack.errors(errs)
		return
	}

	parsed := ParseDynamicSubmission(values, resource.Steps[stepIdx], dynamicKeys)
	if state.DynamicConfig == nil {
		state.DynamicConfig = make(map[string]any)
	}
	for k, v := range parsed {
		state.DynamicConfig[k] = v
	}

	updateTargetKey(state, resource, isCreate, stepIdx, values)

	if stepIdx < len(resource.Steps)-1 {
		next := dynamicCallback{Mode: step.Mode, Step: step.Step + 1}
		modal := BuildDynamicModal(resource, stepIdx+1, state.DynamicConfig, dynamicKeys,
			next.String(), state.ThreadTS+":"+state.Nonce)
		_ = ack.update(modal)
		return
	}

	if isCreate || resource.Kind == schema.KindSingleton {
		captureJustification(state, values)
	}
	_ = ack.send()
	h.queuePR(ctx, state, resource)
}

// isCreateKeyStep returns true when the current step is the first step of a
// map_entry create flow, where the user picks a new key.
func isCreateKeyStep(resource *schema.Resource, isCreate bool, stepIdx int) bool {
	return resource.Kind == schema.KindMapEntry && isCreate && stepIdx == 0
}

func validateCreateKey(resource *schema.Resource, values map[string]map[string]slack.BlockAction, existing []string, errs map[string]string) {
	keyVal := values[BlockResourceKey][ElemResourceKey].Value
	switch {
	case keyVal == "":
		errs[BlockResourceKey] = "Name is required."
		return
	case resource.KeyPattern != "":
		matched, _ := regexp.MatchString(resource.KeyPattern, keyVal)
		if !matched {
			errs[BlockResourceKey] = fmt.Sprintf("Must match pattern: %s", resource.KeyPattern)
			return
		}
	}
	for _, k := range existing {
		if k == keyVal {
			errs[BlockResourceKey] = "A resource with this name already exists."
			return
		}
	}
}

// updateTargetKey records the target identifier on state once the user has
// provided enough information to derive it.
func updateTargetKey(state *conversation.State, resource *schema.Resource, isCreate bool, stepIdx int, values map[string]map[string]slack.BlockAction) {
	switch {
	case isCreateKeyStep(resource, isCreate, stepIdx):
		state.TargetRepo = values[BlockResourceKey][ElemResourceKey].Value
	case resource.Kind == schema.KindSingleton:
		state.TargetRepo = resource.ID
	case resource.Kind == schema.KindMembership:
		state.TargetRepo = fmt.Sprintf("%v/%v", state.DynamicConfig["team"], state.DynamicConfig["username"])
	}
}

func captureJustification(state *conversation.State, values map[string]map[string]slack.BlockAction) {
	if just, ok := values[BlockJustification]; ok {
		if elem, ok := just[ElemJustification]; ok {
			state.Justification = elem.Value
		}
	}
}

func (h *Handler) queuePR(parent context.Context, state *conversation.State, resource *schema.Resource) {
	state.Phase = conversation.PhaseCreatingPR
	go func() {
		bgCtx := workflowContext(parent)
		h.showDynamicConfirmation(bgCtx, state, resource)
		h.createDynamicPR(bgCtx, state)
	}()
}

// viewAcker captures the per-submission ack closure, including logging, span
// status, modal validation, and slow-ack detection.
type viewAcker struct {
	ctx        context.Context
	log        *slog.Logger
	responder  interactionResponder
	span       trace.Span
	state      *conversation.State
	callbackID string
	started    time.Time
}

func newViewAcker(ctx context.Context, log *slog.Logger, responder interactionResponder, span trace.Span, state *conversation.State, callbackID string) *viewAcker {
	return &viewAcker{
		ctx:        ctx,
		log:        log,
		responder:  responder,
		span:       span,
		state:      state,
		callbackID: callbackID,
		started:    time.Now(),
	}
}

func (a *viewAcker) send(payload ...any) error {
	fields := []any{
		"callback_id", a.callbackID,
		"resource_type", a.state.ResourceType,
		"action_type", a.state.ActionType,
		"ack_duration_ms", time.Since(a.started).Milliseconds(),
	}
	if len(payload) == 1 {
		fields = a.augmentFromPayload(fields, payload[0])
		if err := a.validatePayload(payload[0]); err != nil {
			return a.errors(map[string]string{BlockResourceKey: "Generated modal was invalid. Check logs."})
		}
	}
	a.log.InfoContext(a.ctx, "acking view submission", fields...)
	if elapsed := time.Since(a.started); elapsed > slowAckThreshold {
		err := fmt.Errorf("slow Slack ack: %dms", elapsed.Milliseconds())
		a.span.RecordError(err)
		a.span.SetStatus(codes.Error, err.Error())
		captureWorkflowError(a.ctx, a.state, "slow ack", err)
	}
	if err := a.responder.Ack(payload...); err != nil {
		a.span.RecordError(err)
		a.span.SetStatus(codes.Error, err.Error())
		a.log.ErrorContext(a.ctx, "failed to ack view submission", append(fields, "error", err)...)
		captureWorkflowError(a.ctx, a.state, "ack view submission", err)
		return err
	}
	return nil
}

func (a *viewAcker) augmentFromPayload(fields []any, payload any) []any {
	body, ok := payload.(map[string]any)
	if !ok {
		return fields
	}
	fields = append(fields, "response_action", body["response_action"])
	if view, ok := body["view"].(slack.ModalViewRequest); ok {
		fields = append(fields,
			"next_callback_id", view.CallbackID,
			"blocks_count", len(view.Blocks.BlockSet),
			"title", view.Title.Text,
			"private_metadata_len", len(view.PrivateMetadata),
		)
	}
	if raw, err := json.Marshal(body); err == nil {
		fields = append(fields, "payload_size_bytes", len(raw))
	}
	return fields
}

func (a *viewAcker) validatePayload(payload any) error {
	body, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	view, ok := body["view"].(slack.ModalViewRequest)
	if !ok {
		return nil
	}
	if err := validateModalViewRequest(view); err != nil {
		a.span.RecordError(err)
		a.span.SetStatus(codes.Error, err.Error())
		a.log.ErrorContext(a.ctx, "refusing invalid modal ack", "callback_id", a.callbackID, "error", err)
		captureWorkflowError(a.ctx, a.state, "validate modal ack", err)
		return err
	}
	return nil
}

func (a *viewAcker) errors(errs map[string]string) error {
	return a.send(map[string]any{
		"response_action": "errors",
		"errors":          errs,
	})
}

func (a *viewAcker) errorMsg(msg string) error {
	return a.errors(map[string]string{BlockResourceKey: msg})
}

func (a *viewAcker) update(view slack.ModalViewRequest) error {
	return a.send(map[string]any{
		"response_action": "update",
		"view":            view,
	})
}
