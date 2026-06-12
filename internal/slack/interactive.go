package slack

import (
	"context"
	"fmt"

	"github.com/jae-labs/concierge/internal/conversation"
	hcleditor "github.com/jae-labs/concierge/internal/hcl"
	"github.com/jae-labs/concierge/internal/schema"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

const msgFlowExpired = "This flow has expired. Click *New Chat* to start another."

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

	threadTS := callback.Message.ThreadTimestamp
	if threadTS == "" {
		threadTS = callback.Message.Timestamp
	}
	state := h.store.Get(threadTS)
	messageTS := callback.Message.Timestamp

	if err := h.checkBlockAction(ctx, callback, state, messageTS); err != "" {
		h.postEphemeralCtx(ctx, callback.Channel.ID, callback.User.ID, "notify block action denied",
			slack.MsgOptionText(err, false))
		return
	}

	switch action.ActionID {
	case ActionCategorySelect:
		h.handleCategorySelect(ctx, state, action.SelectedOption.Value, messageTS)
	case ActionResourceSelect:
		h.handleResourceSelect(ctx, state, action.SelectedOption.Value, messageTS)
	case ActionActionSelect:
		h.handleActionSelect(ctx, state, action.SelectedOption.Value, callback.TriggerID, messageTS)
	}
}

// checkBlockAction returns an empty string when the block action is allowed, or
// an ephemeral message to show the requester otherwise.
func (h *Handler) checkBlockAction(_ context.Context, callback slack.InteractionCallback, state *conversation.State, messageTS string) string {
	switch {
	case state == nil:
		return msgFlowExpired
	case callback.User.ID != state.UserID:
		return "This session belongs to another user."
	case !h.isAuthorized(callback.User.ID):
		return msgUnauthorized
	case !state.HasMessage(messageTS):
		return msgFlowExpired
	}
	return ""
}

func (h *Handler) handleCategorySelect(ctx context.Context, state *conversation.State, category, messageTS string) {
	state.Category = category
	state.Phase = conversation.PhaseCategorySelected

	runtimeSchema, err := h.fetchRuntimeSchema(ctx)
	if err != nil {
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionText("Schema unavailable. Cannot load resources.", false))
		h.store.Delete(state.ThreadTS)
		return
	}
	resources := runtimeSchema.ResourcesByCategory(category)
	blocks := ResourceBlocksFromOptions(category, ResourceOptionsFromSchema(resources))
	h.replyCtx(ctx, state, conversation.MsgResource, "", slack.MsgOptionBlocks(blocks...))

	label := labelForValue(CategoryOptionsFromSchema(runtimeSchema.Categories), category)
	h.updateMessageCtx(ctx, state, messageTS, slack.MsgOptionBlocks(LockedCategoryBlocks(label)...))

	if len(resources) == 0 {
		h.store.Delete(state.ThreadTS)
	}
}

func (h *Handler) handleResourceSelect(ctx context.Context, state *conversation.State, resource, messageTS string) {
	state.ResourceType = resource
	state.Phase = conversation.PhaseResourceSelected

	resourceSchema, err := h.fetchSchema(ctx, resource)
	if err != nil {
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionBlocks(ComingSoonBlocks(resource)...))
		h.store.Delete(state.ThreadTS)
		return
	}
	blocks := ActionBlocksFromOptions(resource, ActionOptionsFromSchema(*resourceSchema))
	h.replyCtx(ctx, state, conversation.MsgAction, "", slack.MsgOptionBlocks(blocks...))

	label := labelForValue(ResourceOptionsFromSchema(h.schemaResourcesByCategory(ctx, state.Category)), resource)
	h.updateMessageCtx(ctx, state, messageTS, slack.MsgOptionBlocks(LockedResourceBlocks(state.Category, label)...))
}

func (h *Handler) handleActionSelect(ctx context.Context, state *conversation.State, actionType, triggerID, messageTS string) {
	state.ActionType = actionType
	state.Phase = conversation.PhaseActionSelected

	resourceSchema, err := h.fetchSchema(ctx, state.ResourceType)
	if err != nil {
		h.replyCtx(ctx, state, conversation.MsgProgress, "", slack.MsgOptionBlocks(ComingSoonBlocks(state.ResourceType)...))
		h.store.Delete(state.ThreadTS)
		return
	}
	h.openActionModal(ctx, state, resourceSchema, actionType, triggerID)

	label := labelForValue(ActionOptionsFromSchema(*resourceSchema), actionType)
	h.updateMessageCtx(ctx, state, messageTS, slack.MsgOptionBlocks(LockedActionBlocks(label)...))
}

// openActionModal launches the correct dynamic modal for a resource+action
// combination, fetching existing values for singleton/update flows up front.
func (h *Handler) openActionModal(ctx context.Context, state *conversation.State, resource *schema.Resource, actionType, triggerID string) {
	metadata := state.ThreadTS + ":" + state.Nonce
	state.DynamicKeys = h.fetchDynamicKeys(ctx, resource)
	state.DynamicFileContent = nil
	state.DynamicResourceKeys = nil

	open := func(modal slack.ModalViewRequest, kind string) {
		if err := h.openView(ctx, triggerID, modal); err != nil {
			h.logger.Error("failed to open "+kind, "error", err, "user", state.UserID)
			h.replyCtx(ctx, state, conversation.MsgProgress, "",
				slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	}
	replyFetchFailure := func(err error) {
		h.replyCtx(ctx, state, conversation.MsgProgress, "",
			slack.MsgOptionText(fmt.Sprintf("Failed to fetch file: %v", err), false))
	}

	switch resource.Kind {
	case schema.KindSingleton:
		existingValues, err := h.fetchExistingDynamicValues(ctx, resource, "")
		if err != nil {
			replyFetchFailure(err)
			return
		}
		state.DynamicConfig = existingValues
		open(BuildDynamicModal(resource, 0, existingValues, state.DynamicKeys,
			dynamicCallback{Mode: flowUpdate, Step: 1}.String(), metadata), "singleton dynamic modal")

	case schema.KindMembership:
		mode := flowUpdate
		if actionType == schema.ActionAdd {
			mode = flowCreate
		}
		open(BuildDynamicModal(resource, 0, nil, state.DynamicKeys,
			dynamicCallback{Mode: mode, Step: 1}.String(), metadata), "membership dynamic modal")

	default:
		if actionType == schema.ActionAdd {
			open(BuildDynamicModal(resource, 0, nil, state.DynamicKeys,
				dynamicCallback{Mode: flowCreate, Step: 1}.String(), metadata), "dynamic modal")
			return
		}
		src, _, err := h.gh.GetFileContent(ctx, resource.File)
		if err != nil {
			replyFetchFailure(err)
			return
		}
		keys, err := hcleditor.ExistingResourceKeys(src, resource.RootPath)
		if err != nil || len(keys) == 0 {
			h.replyCtx(ctx, state, conversation.MsgProgress, "",
				slack.MsgOptionText("No existing resources found to modify or delete.", false))
			return
		}
		state.DynamicFileContent = src
		state.DynamicResourceKeys = keys
		open(BuildDynamicSelectModal(resource, actionType, keys, CallbackDynamicSelectTarget, metadata), "select modal")
	}
}

func (h *Handler) fetchExistingDynamicValues(ctx context.Context, resource *schema.Resource, targetKey string) (map[string]any, error) {
	src, _, err := h.gh.GetFileContent(ctx, resource.File)
	if err != nil {
		return nil, err
	}
	if resource.Kind == schema.KindSingleton {
		return hcleditor.ReadSingleton(src, resource.RootPath, resource)
	}
	return hcleditor.ReadResource(src, resource.RootPath, targetKey, resource)
}

// fetchDynamicKeys pulls the dynamic option lists used by select / map_string
// fields whose options come from another Terraform locals path.
func (h *Handler) fetchDynamicKeys(ctx context.Context, resource *schema.Resource) map[string][]string {
	dynamicKeys := make(map[string][]string)
	for _, step := range resource.Steps {
		for _, field := range step.Fields {
			if field.KeySource == nil {
				continue
			}
			if field.Type != schema.TypeMapString && field.Type != schema.TypeSelect {
				continue
			}
			src, _, err := h.gh.GetFileContent(ctx, field.KeySource.File)
			if err != nil {
				continue
			}
			keys, err := hcleditor.ExistingResourceKeys(src, field.KeySource.RootPath)
			if err == nil {
				dynamicKeys[field.Path] = keys
			}
		}
	}
	return dynamicKeys
}

// cachedDynamicKeys returns the keys already snapshot on the state, or falls
// back to a fresh fetch when none are cached (e.g. tests bypassing setup).
func cachedDynamicKeys(state *conversation.State, fallback func() map[string][]string) map[string][]string {
	if len(state.DynamicKeys) > 0 {
		return state.DynamicKeys
	}
	return fallback()
}
