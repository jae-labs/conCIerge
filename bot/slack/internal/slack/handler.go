package slack

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jae-labs/opsy/internal/conversation"
	ghclient "github.com/jae-labs/opsy/internal/github"
	hcleditor "github.com/jae-labs/opsy/internal/hcl"
	linearclient "github.com/jae-labs/opsy/internal/linear"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	pathGitHubRepos   = "iac/terraform/github/locals_repos.tf"
	pathGitHubMembers = "iac/terraform/github/locals_members.tf"
	pathGitHubOrg     = "iac/terraform/github/locals_org.tf"
	pathCloudflareDNS = "iac/terraform/cloudflare/locals_dns.tf"
)

// userIDs is the set of Slack user IDs allowed to use the bot.
// isApprover checks if a user can approve requests (manager or admin).
func (h *Handler) isApprover(userID string) bool {
	return h.managerIDs[userID] || h.adminIDs[userID]
}

// isAuthorized checks if a user can use the bot (user, manager, or admin).
func (h *Handler) isAuthorized(userID string) bool {
	return h.userIDs[userID] || h.managerIDs[userID] || h.adminIDs[userID]
}

var prURLPattern = regexp.MustCompile(`/pull/(\d+)`)

type Handler struct {
	api               *slack.Client
	sm                *socketmode.Client
	store             *conversation.Store
	gh                *ghclient.Client
	linear            *linearclient.Client
	logger            *slog.Logger
	requestsChannelID string
	userIDs           map[string]bool
	managerIDs        map[string]bool
	adminIDs          map[string]bool
}

func NewHandler(api *slack.Client, sm *socketmode.Client, gh *ghclient.Client, linear *linearclient.Client, requestsChannelID string, userIDs, managerIDs, adminIDs map[string]bool, logger *slog.Logger) *Handler {
	return &Handler{
		api:               api,
		sm:                sm,
		store:             conversation.NewStore(),
		gh:                gh,
		linear:            linear,
		requestsChannelID: requestsChannelID,
		userIDs:           userIDs,
		managerIDs:        managerIDs,
		adminIDs:          adminIDs,
		logger:            logger,
	}
}

func (h *Handler) Run() {
	go h.eventLoop()
	h.sm.Run()
}

// reply sends a message in the assistant thread and tracks it.
func (h *Handler) reply(state *conversation.State, kind conversation.MessageKind, label string, opts ...slack.MsgOption) string {
	opts = append(opts, slack.MsgOptionTS(state.ThreadTS))
	_, ts, _ := h.api.PostMessage(state.ChannelID, opts...)
	if ts != "" {
		state.TrackMessage(ts, kind, label)
	}
	return ts
}

// replyPR creates a Linear issue, posts the request summary to #opsy-requests, then closes the chat.
func (h *Handler) replyPR(state *conversation.State, prTitle, prURL string) {
	current := h.store.Get(state.ThreadTS)
	superseded := current == nil || current.Nonce != state.Nonce

	// resolve requester name for Linear issue
	ctx := context.Background()
	requesterName := state.UserID
	if user, err := h.api.GetUserInfo(state.UserID); err == nil {
		requesterName = user.RealName
	}

	// create Linear issue
	issueDesc := buildLinearIssueDescription(state, requesterName, prURL)
	linearIssue, err := h.linear.CreateIssue(ctx, prTitle, issueDesc, linearclient.LinearPriority(state.Priority))
	if err != nil {
		h.logger.Error("failed to create linear issue", "error", err)
	}

	// add Linear issue reference to the PR body so merge auto-closes it
	if linearIssue != nil {
		matches := prURLPattern.FindStringSubmatch(prURL)
		if len(matches) >= 2 {
			prNumber, _ := strconv.Atoi(matches[1])
			appendText := fmt.Sprintf("Closes [%s](%s)", linearIssue.Identifier, linearIssue.URL)
			if err := h.gh.UpdatePRBody(ctx, prNumber, appendText); err != nil {
				h.logger.Error("failed to add linear reference to PR body", "error", err)
			}
		}
	}

	// build summary for #opsy-requests
	summary := buildRequestSummary(state, prTitle, prURL, linearIssue)
	_, msgTS, err := h.api.PostMessage(h.requestsChannelID,
		slack.MsgOptionBlocks(summary...),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl())
	if err != nil {
		h.logger.Error("failed to post to requests channel", "error", err)
	}

	// build permalink to the #opsy-requests message
	var link string
	if msgTS != "" {
		permalink, err := h.api.GetPermalink(&slack.PermalinkParameters{
			Channel: h.requestsChannelID,
			Ts:      msgTS,
		})
		if err == nil {
			link = permalink
		}
	}

	// close the chat with link to the request
	var closingText string
	if link != "" {
		closingText = fmt.Sprintf("Request submitted: <%s|View your request>\n\n*Next steps:* follow up with your manager to approve the request above following instructions displayed on it.\n\nThis chat has ended. Open a *New Chat* if you need to raise a new request.", link)
	} else {
		closingText = fmt.Sprintf("Request submitted to <#%s>.\n\n*Next steps:* follow up with your manager to approve the request above following instructions displayed on it.\n\nThis chat has ended. Open a *New Chat* if you need to raise a new request.", h.requestsChannelID)
	}
	h.api.PostMessage(state.ChannelID,
		slack.MsgOptionText(closingText, false),
		slack.MsgOptionTS(state.ThreadTS),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl())

	if !superseded {
		h.store.Delete(state.ThreadTS)
	}
}

// updateMessage replaces the blocks of an existing message (used to lock dropdowns).
func (h *Handler) updateMessage(state *conversation.State, messageTS string, opts ...slack.MsgOption) {
	_, _, _, _ = h.api.UpdateMessage(state.ChannelID, messageTS, opts...)
}

func (h *Handler) eventLoop() {
	for evt := range h.sm.Events {
		switch evt.Type {
		case socketmode.EventTypeEventsAPI:
			h.handleEventsAPI(evt)
		case socketmode.EventTypeInteractive:
			h.handleInteractive(evt)
		default:
			h.logger.Debug("unhandled event type", "type", evt.Type)
		}
	}
}

func (h *Handler) handleEventsAPI(evt socketmode.Event) {
	eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}
	h.sm.Ack(*evt.Request)

	h.logger.Debug("events API received", "inner_type", eventsAPI.InnerEvent.Type)

	switch ev := eventsAPI.InnerEvent.Data.(type) {
	case *slackevents.AppHomeOpenedEvent:
		h.handleAppHomeOpened(ev)
	case *slackevents.AssistantThreadStartedEvent:
		h.handleAssistantThreadStarted(ev)
	case *slackevents.MessageEvent:
		if ev.ChannelType != "im" || ev.BotID != "" || ev.SubType != "" {
			return
		}
		if ev.ThreadTimeStamp != "" {
			h.handleThreadReply(ev.User, ev.Channel, ev.Text, ev.ThreadTimeStamp)
		} else {
			// top-level DM: treat as its own thread (fallback when assistant threads aren't active)
			h.handleNewFlow(ev.User, ev.Channel, ev.TimeStamp)
		}
	case *slackevents.ReactionAddedEvent:
		if ev.Reaction == "+1" || ev.Reaction == "thumbsup" {
			h.handlePRApproval(ev.User, ev.Item.Channel, ev.Item.Timestamp)
		}
	}
}

// handleAppHomeOpened publishes the Home tab view when a user opens the app.
func (h *Handler) handleAppHomeOpened(ev *slackevents.AppHomeOpenedEvent) {
	if ev.Tab != "home" {
		return
	}

	view := slack.HomeTabViewRequest{
		Type: slack.VTHomeTab,
		Blocks: slack.Blocks{
			BlockSet: HomeTabBlocks(ev.User),
		},
	}
	if _, err := h.api.PublishView(ev.User, view, ""); err != nil {
		h.logger.Error("failed to publish home tab", "error", err, "user", ev.User)
	}
}

// handleAssistantThreadStarted creates a new flow when user clicks "New Chat".
func (h *Handler) handleAssistantThreadStarted(ev *slackevents.AssistantThreadStartedEvent) {
	threadTS := ev.AssistantThread.ThreadTimeStamp
	channelID := ev.AssistantThread.ChannelID
	userID := ev.AssistantThread.UserID

	if !h.isAuthorized(userID) {
		h.api.PostMessage(channelID,
			slack.MsgOptionText("You are not authorized to use Opsy. Contact an admin for access.", false),
			slack.MsgOptionTS(threadTS))
		return
	}

	state := h.store.Create(threadTS, channelID, userID)

	_, ts, err := h.api.PostMessage(channelID,
		slack.MsgOptionBlocks(WelcomeBlocks(userID)...),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		h.logger.Error("failed to send welcome", "error", err, "user", userID)
		return
	}
	state.TrackMessage(ts, conversation.MsgWelcome, "")
}

// handleNewFlow starts a fresh flow threaded to the user's top-level message.
// Used as fallback when assistant_thread_started is not available.
func (h *Handler) handleNewFlow(userID, channelID, messageTS string) {
	if !h.isAuthorized(userID) {
		h.api.PostMessage(channelID,
			slack.MsgOptionText("You are not authorized to use Opsy. Contact an admin for access.", false),
			slack.MsgOptionTS(messageTS))
		return
	}

	state := h.store.Create(messageTS, channelID, userID)

	_, ts, err := h.api.PostMessage(channelID,
		slack.MsgOptionBlocks(WelcomeBlocks(userID)...),
		slack.MsgOptionTS(messageTS),
	)
	if err != nil {
		h.logger.Error("failed to send welcome", "error", err, "user", userID)
		return
	}
	state.TrackMessage(ts, conversation.MsgWelcome, "")
}

// handleThreadReply handles text typed inside a flow's thread (e.g. "cancel").
func (h *Handler) handleThreadReply(userID, channelID, text, threadTS string) {
	state := h.store.Get(threadTS)
	if state == nil {
		h.api.PostMessage(channelID,
			slack.MsgOptionText("This session is no longer valid. Please open a new chat.", false),
			slack.MsgOptionTS(threadTS))
		return
	}

	if strings.EqualFold(strings.TrimSpace(text), "cancel") {
		go h.lockFlowMessages(state)
		h.store.Delete(threadTS)
		h.api.PostMessage(channelID,
			slack.MsgOptionText("This session is no longer valid. Please open a new chat.", false),
			slack.MsgOptionTS(threadTS))
		return
	}

	h.api.PostMessage(channelID,
		slack.MsgOptionText("This session is no longer valid. Please open a new chat.", false),
		slack.MsgOptionTS(threadTS))
	go h.lockFlowMessages(state)
	h.store.Delete(threadTS)
}

func (h *Handler) handleInteractive(evt socketmode.Event) {
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		return
	}

	switch callback.Type {
	case slack.InteractionTypeBlockActions:
		h.sm.Ack(*evt.Request)
		h.handleBlockAction(callback)
	case slack.InteractionTypeViewSubmission:
		h.handleViewSubmission(evt, callback)
	default:
		h.sm.Ack(*evt.Request)
	}
}

func (h *Handler) handleBlockAction(callback slack.InteractionCallback) {
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
		h.api.PostEphemeral(callback.Channel.ID, callback.User.ID,
			slack.MsgOptionText("This flow has expired. Click *New Chat* to start another.", false))
		return
	}

	messageTS := callback.Message.Timestamp

	if !state.HasMessage(messageTS) {
		h.api.PostEphemeral(callback.Channel.ID, callback.User.ID,
			slack.MsgOptionText("This flow has expired. Click *New Chat* to start another.", false))
		return
	}

	switch action.ActionID {
	case ActionCategorySelect:
		h.handleCategorySelect(state, action.SelectedOption.Value, messageTS)
	case ActionResourceSelect:
		h.handleResourceSelect(state, action.SelectedOption.Value, callback.TriggerID, messageTS)
	case ActionActionSelect:
		h.handleActionSelect(state, action.SelectedOption.Value, callback.TriggerID, messageTS)
	}
}

func (h *Handler) handleCategorySelect(state *conversation.State, category, messageTS string) {
	state.Category = category
	state.Phase = conversation.PhaseCategorySelected

	blocks := ResourceBlocks(category)
	h.reply(state, conversation.MsgResource, "", slack.MsgOptionBlocks(blocks...))

	label := labelForValue(categories, category)
	h.updateMessage(state, messageTS, slack.MsgOptionBlocks(LockedCategoryBlocks(label)...))

	// if no resources defined for this category, ResourceBlocks returns ComingSoon and flow ends
	if _, ok := resourceOptions[category]; !ok {
		h.store.Delete(state.ThreadTS)
	}
}

func (h *Handler) handleResourceSelect(state *conversation.State, resource, triggerID, messageTS string) {
	state.ResourceType = resource
	state.Phase = conversation.PhaseResourceSelected

	switch resource {
	case "repo", "dns", "user_management":
		blocks := ActionBlocks(resource)
		h.reply(state, conversation.MsgAction, "", slack.MsgOptionBlocks(blocks...))
	case "org_settings":
		h.handleOrgSettingsResource(state, triggerID)
	default:
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionBlocks(ComingSoonBlocks(resource)...))
		h.store.Delete(state.ThreadTS)
	}

	label := labelForValue(resourceOptions[state.Category], resource)
	h.updateMessage(state, messageTS, slack.MsgOptionBlocks(LockedResourceBlocks(state.Category, label)...))
}

func (h *Handler) handleActionSelect(state *conversation.State, actionType, triggerID, messageTS string) {
	state.ActionType = actionType
	state.Phase = conversation.PhaseActionSelected

	switch state.ResourceType {
	case "dns":
		h.handleDnsAction(state, actionType, triggerID)
	case "user_management":
		h.handleUserManagementAction(state, actionType, triggerID)
	default:
		h.handleRepoAction(state, actionType, triggerID)
	}

	label := labelForValue(actionOptions[state.ResourceType], actionType)
	h.updateMessage(state, messageTS, slack.MsgOptionBlocks(LockedActionBlocks(label)...))
}

func (h *Handler) handleRepoAction(state *conversation.State, actionType, triggerID string) {
	switch actionType {
	case "add":
		modal := RepoStep1Modal()
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		_, err := h.api.OpenView(triggerID, modal)
		if err != nil {
			h.logger.Error("failed to open modal", "error", err, "user", state.UserID)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "delete":
		repos := h.fetchRepoNames()
		if len(repos) == 0 {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("No repositories found. There's nothing to remove.", false))
			return
		}
		modal := DeleteRepoModal(repos)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		_, err := h.api.OpenView(triggerID, modal)
		if err != nil {
			h.logger.Error("failed to open delete modal", "error", err, "user", state.UserID)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "settings":
		repos := h.fetchRepoNames()
		if len(repos) == 0 {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("No repositories found. There's nothing to update.", false))
			return
		}
		modal := SelectRepoModal(repos)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		_, err := h.api.OpenView(triggerID, modal)
		if err != nil {
			h.logger.Error("failed to open select repo modal", "error", err, "user", state.UserID)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	}
}

func (h *Handler) handleDnsAction(state *conversation.State, actionType, triggerID string) {
	src, err := h.fetchCloudflareHCL()
	if err != nil {
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch Cloudflare configuration.", false))
		h.store.Delete(state.ThreadTS)
		return
	}

	zones, err := hcleditor.ExistingZones(src)
	if err != nil || len(zones) == 0 {
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("No DNS zones found.", false))
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
		if _, err := h.api.OpenView(triggerID, modal); err != nil {
			h.logger.Error("failed to open dns add modal", "error", err)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "delete":
		records := h.fetchDnsRecordOptions(src, zone)
		if len(records) == 0 {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("No DNS records found. There's nothing to remove.", false))
			return
		}
		modal := DnsRemoveModal(zone, records)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		if _, err := h.api.OpenView(triggerID, modal); err != nil {
			h.logger.Error("failed to open dns remove modal", "error", err)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	case "settings":
		records := h.fetchDnsRecordOptions(src, zone)
		if len(records) == 0 {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("No DNS records found. There's nothing to update.", false))
			return
		}
		modal := DnsSelectRecordModal(zone, records)
		modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
		if _, err := h.api.OpenView(triggerID, modal); err != nil {
			h.logger.Error("failed to open dns select record modal", "error", err)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
		}
	}
}

func (h *Handler) handleOrgSettingsResource(state *conversation.State, triggerID string) {
	src, err := h.fetchOrgHCLSource()
	if err != nil {
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch GitHub configuration.", false))
		h.store.Delete(state.ThreadTS)
		return
	}

	cfg, err := hcleditor.ExtractOrgSettings(src)
	if err != nil {
		h.logger.Error("failed to extract org settings", "error", err)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to read org settings from configuration.", false))
		h.store.Delete(state.ThreadTS)
		return
	}
	state.OrgConfig = cfg

	modal := OrgSettingsModal(state.OrgConfig)
	modal.PrivateMetadata = state.ThreadTS + ":" + state.Nonce
	if _, err := h.api.OpenView(triggerID, modal); err != nil {
		h.logger.Error("failed to open org settings modal", "error", err)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
	}
}

func (h *Handler) handleViewSubmission(evt socketmode.Event, callback slack.InteractionCallback) {
	// PrivateMetadata format: "threadTS:nonce"
	parts := strings.SplitN(callback.View.PrivateMetadata, ":", 2)
	if len(parts) != 2 {
		h.logger.Warn("invalid PrivateMetadata format", "metadata", callback.View.PrivateMetadata)
		h.sm.Ack(*evt.Request)
		return
	}
	threadTS, nonce := parts[0], parts[1]

	state := h.store.Get(threadTS)
	if state == nil || state.Nonce != nonce {
		h.logger.Warn("no flow or nonce mismatch for modal submission", "thread_ts", threadTS)
		h.sm.Ack(*evt.Request)
		return
	}

	values := callback.View.State.Values

	h.logger.Info("view submission received",
		"callback_id", callback.View.CallbackID,
		"user", state.UserID,
		"thread_ts", threadTS,
	)

	switch callback.View.CallbackID {
	case CallbackRepoStep1:
		if errs := validateRepoStep1(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		repoName := values[BlockName][ElemName].Value
		if msg := h.checkRepoAlreadyExists(repoName); msg != "" {
			h.sm.Ack(*evt.Request, map[string]interface{}{
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

		h.logger.Info("step1 parsed",
			"name", state.RepoConfig.Name,
			"description", state.RepoConfig.Description,
			"visibility", state.RepoConfig.Visibility,
		)

		state.AvailableTeams = h.fetchTeamNames()
		modal := RepoStep2Modal(state.AvailableTeams)
		modal.PrivateMetadata = threadTS + ":" + state.Nonce

		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		h.sm.Ack(*evt.Request, resp)

	case CallbackRepoStep2:
		if errs := validateRepoStep2(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
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

		h.logger.Info("step2 parsed",
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
		h.sm.Ack(*evt.Request, resp)

	case CallbackRepoStep3:
		if errs := validateRepoStep3(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		h.parseStep3Values(state, values)
		h.sm.Ack(*evt.Request)

		rc := state.RepoConfig
		summary := RepoCreateSummary(
			rc.Name, rc.Description, rc.Visibility, rc.Topics, rc.TeamAccess,
			rc.DefaultBranch, rc.HasIssues, rc.EnableBranchProtection,
			rc.DismissStaleReviews, rc.RequireLinearHistory, rc.RequireConversationResolution,
			rc.RequiredReviews, rc.AllowAutoMerge, rc.AllowUpdateBranch, rc.DeleteBranchOnMerge,
			rc.HasDiscussions, rc.HasProjects, rc.HomepageURL,
			state.Justification,
		)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createPR(state)

	case CallbackDeleteRepo:
		targetRepo := values[BlockDeleteTarget][ElemDeleteTarget].SelectedOption.Value
		if msg := h.checkRepoStillExists(targetRepo); msg != "" {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockDeleteTarget: msg},
			})
			return
		}
		state.TargetRepo = targetRepo
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		h.sm.Ack(*evt.Request)

		summary := RepoDeleteSummary(state.TargetRepo, state.Justification)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDeletePR(state)

	case CallbackSelectRepo:
		repoName := values[BlockSelectRepo][ElemSelectRepo].SelectedOption.Value
		state.TargetRepo = repoName

		src, err := h.fetchHCLSource()
		if err != nil {
			h.logger.Error("failed to fetch repos HCL for settings", "error", err)
			h.sm.Ack(*evt.Request)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch the repository configuration.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		cfg, err := hcleditor.ExtractRepoConfig(src, repoName)
		if err != nil {
			h.logger.Error("failed to extract repo config", "error", err, "repo", repoName)
			h.sm.Ack(*evt.Request)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not read config for %s: %v", repoName, err), false))
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
		h.sm.Ack(*evt.Request, resp)

	case CallbackSettingsStep1:
		if errs := validateSettingsStep1(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
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

		state.AvailableTeams = h.fetchTeamNames()
		modal := SettingsStep2Modal(state.RepoConfig, state.AvailableTeams)
		modal.PrivateMetadata = threadTS + ":" + state.Nonce
		resp := map[string]interface{}{
			"response_action": "update",
			"view":            modal,
		}
		h.sm.Ack(*evt.Request, resp)

	case CallbackSettingsStep2:
		if errs := validateSettingsStep2(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
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
		h.sm.Ack(*evt.Request, resp)

	case CallbackSettingsStep3:
		if errs := validateRepoStep3(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}
		h.parseStep3Values(state, values)
		h.sm.Ack(*evt.Request)

		// fetch fresh HCL to get old config for comparison
		src, err := h.fetchHCLSource()
		if err != nil {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch current config for comparison.", false))
			h.store.Delete(state.ThreadTS)
			return
		}
		oldCfg, err := hcleditor.ExtractRepoConfig(src, state.TargetRepo)
		if err != nil {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not extract current settings for %s.", state.TargetRepo), false))
			h.store.Delete(state.ThreadTS)
			return
		}

		// check if anything changed
		if repoConfigEqual(oldCfg, state.RepoConfig) {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Nothing has changed. No PR needed.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		summary := RepoSettingsSummary(state.TargetRepo, oldCfg, state.RepoConfig, state.Justification)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createSettingsPR(state)

	// --- DNS flow callbacks ---

	case CallbackDnsAdd:
		if errs := validateDnsFields(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          errs,
			})
			return
		}

		newType := values[BlockDnsType][ElemDnsType].SelectedOption.Value
		newName := values[BlockDnsName][ElemDnsName].Value

		// check for DNS conflicts against live data
		if conflict := h.checkDnsAddConflict(state.TargetZone, newName, newType); conflict != "" {
			h.sm.Ack(*evt.Request, map[string]interface{}{
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
		h.sm.Ack(*evt.Request)

		summary := DnsAddSummary(state.TargetZone, state.DnsConfig, state.Justification)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDnsAddPR(state)

	case CallbackDnsRemove:
		recordKey := values[BlockDnsRecord][ElemDnsRecord].SelectedOption.Value

		// verify the record still exists before proceeding
		if msg := h.checkDnsRecordStillExists(state.TargetZone, recordKey); msg != "" {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockDnsRecord: msg},
			})
			return
		}

		state.TargetRecord = recordKey
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		h.sm.Ack(*evt.Request)

		summary := DnsRemoveSummary(state.TargetZone, state.TargetRecord, state.Justification)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDnsRemovePR(state)

	case CallbackDnsSelectRecord:
		recordKey := values[BlockDnsRecord][ElemDnsRecord].SelectedOption.Value
		state.TargetRecord = recordKey

		src, err := h.fetchCloudflareHCL()
		if err != nil {
			h.logger.Error("failed to fetch DNS HCL for dns update", "error", err)
			h.sm.Ack(*evt.Request)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch DNS configuration.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		cfg, err := hcleditor.ExtractDnsConfig(src, state.TargetZone, recordKey)
		if err != nil {
			h.logger.Error("failed to extract dns config", "error", err, "record", recordKey)
			h.sm.Ack(*evt.Request)
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not read config for %s: %v", recordKey, err), false))
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
		h.sm.Ack(*evt.Request, resp)

	case CallbackDnsUpdate:
		if errs := validateDnsFields(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
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
		h.sm.Ack(*evt.Request)

		// fetch fresh HCL for old config comparison
		src, err := h.fetchCloudflareHCL()
		if err != nil {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch current DNS config for comparison.", false))
			h.store.Delete(state.ThreadTS)
			return
		}
		oldCfg, err := hcleditor.ExtractDnsConfig(src, state.TargetZone, state.TargetRecord)
		if err != nil {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Could not extract current config for %s.", state.TargetRecord), false))
			h.store.Delete(state.ThreadTS)
			return
		}

		if dnsConfigEqual(oldCfg, state.DnsConfig) {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Nothing has changed. No PR needed.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		summary := DnsUpdateSummary(state.TargetZone, oldCfg, state.DnsConfig, state.Justification)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createDnsUpdatePR(state)

	// --- Org Settings flow callback ---

	case CallbackOrgSettings:
		if errs := validateOrgSettings(values); len(errs) > 0 {
			h.sm.Ack(*evt.Request, map[string]interface{}{
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
		h.sm.Ack(*evt.Request)

		// fetch fresh HCL for comparison
		src, err := h.fetchOrgHCLSource()
		if err != nil {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Failed to fetch current config for comparison.", false))
			h.store.Delete(state.ThreadTS)
			return
		}
		oldCfg, err := hcleditor.ExtractOrgSettings(src)
		if err != nil {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Could not extract current org settings.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		if orgConfigEqual(oldCfg, state.OrgConfig) {
			h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Nothing has changed. No PR needed.", false))
			h.store.Delete(state.ThreadTS)
			return
		}

		summary := OrgSettingsSummary(oldCfg, state.OrgConfig, state.Justification)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(summary, false))
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		state.Phase = conversation.PhaseCreatingPR
		go h.createOrgSettingsPR(state)

	// --- User Management flow callbacks ---

	case CallbackTeamMemberAdd:
		team := values[BlockTeamSelect][ElemTeamSelect].SelectedOption.Value
		username := values[BlockMemberSelect][ElemMemberSelect].SelectedOption.Value
		role := values[BlockRoleSelect][ElemRoleSelect].SelectedOption.Value
		if errMsg := validateTeamMemberAdd(team, username, role); errMsg != "" {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockTeamSelect: errMsg},
			})
			return
		}
		state.TeamMemberConfig = conversation.TeamMemberConfig{Team: team, Username: username, Role: role}
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		state.Phase = conversation.PhaseCreatingPR
		h.sm.Ack(*evt.Request)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		go h.createTeamMemberPR(state)

	case CallbackTeamMemberRemove:
		team := values[BlockTeamSelect][ElemTeamSelect].SelectedOption.Value
		username := values[BlockMemberSelect][ElemMemberSelect].SelectedOption.Value
		if errMsg := validateTeamMemberRemove(team, username); errMsg != "" {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockTeamSelect: errMsg},
			})
			return
		}
		state.TeamMemberConfig = conversation.TeamMemberConfig{Team: team, Username: username}
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		state.Phase = conversation.PhaseCreatingPR
		h.sm.Ack(*evt.Request)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		go h.createTeamMemberPR(state)

	case CallbackTeamMemberChangeRole:
		team := values[BlockTeamSelect][ElemTeamSelect].SelectedOption.Value
		username := values[BlockMemberSelect][ElemMemberSelect].SelectedOption.Value
		role := values[BlockRoleSelect][ElemRoleSelect].SelectedOption.Value
		if errMsg := validateTeamMemberChangeRole(team, username, role); errMsg != "" {
			h.sm.Ack(*evt.Request, map[string]interface{}{
				"response_action": "errors",
				"errors":          map[string]string{BlockTeamSelect: errMsg},
			})
			return
		}
		state.TeamMemberConfig = conversation.TeamMemberConfig{Team: team, Username: username, Role: role}
		state.Justification = values[BlockJustification][ElemJustification].Value
		state.Priority = values[BlockPriority][ElemPriority].SelectedOption.Value
		state.Phase = conversation.PhaseCreatingPR
		h.sm.Ack(*evt.Request)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("Processing your request...", false))
		go h.createTeamMemberPR(state)

	default:
		h.sm.Ack(*evt.Request)
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

func (h *Handler) handlePRApproval(userID, channelID, messageTS string) {
	if !h.isApprover(userID) {
		return
	}

	// fetch the message to extract the PR URL from its content
	msgs, err := h.api.GetConversationHistory(&slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    messageTS,
		Inclusive: true,
		Limit:     1,
	})
	if err != nil || len(msgs.Messages) == 0 {
		h.logger.Error("failed to fetch message for PR approval", "error", err, "channel", channelID, "ts", messageTS)
		return
	}

	// extract PR URL from the message text
	msgText := msgs.Messages[0].Text
	matches := prURLPattern.FindStringSubmatch(msgText)
	if len(matches) < 2 {
		// not an opsy request message
		return
	}
	prNumber, _ := strconv.Atoi(matches[1])

	user, err := h.api.GetUserInfo(userID)
	approverName := userID
	if err == nil {
		approverName = user.RealName
	}

	ctx := context.Background()
	body := fmt.Sprintf("Approved via Slack by %s", approverName)
	if err := h.gh.CommentOnPR(ctx, prNumber, body); err != nil {
		h.logger.Error("failed to comment on PR", "error", err, "pr", prNumber)
		h.api.PostMessage(channelID,
			slack.MsgOptionText(fmt.Sprintf("Failed to comment on PR #%d: %s", prNumber, err), false),
			slack.MsgOptionTS(messageTS))
		return
	}

	// thread reply on the #opsy-requests message
	h.api.PostMessage(channelID,
		slack.MsgOptionText(fmt.Sprintf(":white_check_mark: PR #%d has been approved by %s.", prNumber, approverName), false),
		slack.MsgOptionTS(messageTS))

	h.api.PostMessage(channelID,
		slack.MsgOptionText("This request/PR is now pending review and merge to main by an Admin.", false),
		slack.MsgOptionTS(messageTS))
}

// writeResourceDetails appends resource-specific details to a string builder.
func writeResourceDetails(sb *strings.Builder, state *conversation.State) {
	switch state.ResourceType {
	case "repo":
		switch state.ActionType {
		case "add":
			sb.WriteString(fmt.Sprintf("*Repository:* %s\n", state.RepoConfig.Name))
			if state.RepoConfig.Description != "" {
				sb.WriteString(fmt.Sprintf("*Description:* %s\n", state.RepoConfig.Description))
			}
			sb.WriteString(fmt.Sprintf("*Visibility:* %s\n", state.RepoConfig.Visibility))
		case "delete":
			sb.WriteString(fmt.Sprintf("*Repository:* %s\n", state.TargetRepo))
		case "settings":
			sb.WriteString(fmt.Sprintf("*Repository:* %s\n", state.TargetRepo))
		}
	case "dns":
		switch state.ActionType {
		case "add":
			sb.WriteString(fmt.Sprintf("*Zone:* %s\n", state.TargetZone))
			sb.WriteString(fmt.Sprintf("*Record:* %s (%s) -> %s\n", state.DnsConfig.Name, state.DnsConfig.Type, state.DnsConfig.Content))
		case "delete":
			sb.WriteString(fmt.Sprintf("*Zone:* %s\n", state.TargetZone))
			sb.WriteString(fmt.Sprintf("*Record:* %s\n", state.TargetRecord))
		case "settings":
			sb.WriteString(fmt.Sprintf("*Zone:* %s\n", state.TargetZone))
			sb.WriteString(fmt.Sprintf("*Record:* %s\n", state.TargetRecord))
		}
	case "org_settings":
		sb.WriteString("*Resource:* Organization settings\n")
	case "user_management":
		sb.WriteString(fmt.Sprintf("*Team:* %s\n", state.TeamMemberConfig.Team))
		sb.WriteString(fmt.Sprintf("*Member:* %s\n", state.TeamMemberConfig.Username))
		if state.ActionType != "delete" {
			sb.WriteString(fmt.Sprintf("*Role:* %s\n", state.TeamMemberConfig.Role))
		}
	}
}

// writeBulletResourceDetails appends resource details as bullet points.
func writeBulletResourceDetails(sb *strings.Builder, state *conversation.State) {
	switch state.ResourceType {
	case "repo":
		switch state.ActionType {
		case "add":
			sb.WriteString(fmt.Sprintf("• *Repository:* %s\n", state.RepoConfig.Name))
			if state.RepoConfig.Description != "" {
				sb.WriteString(fmt.Sprintf("• *Description:* %s\n", state.RepoConfig.Description))
			}
			sb.WriteString(fmt.Sprintf("• *Visibility:* %s\n", state.RepoConfig.Visibility))
		case "delete":
			sb.WriteString(fmt.Sprintf("• *Repository:* %s\n", state.TargetRepo))
		case "settings":
			sb.WriteString(fmt.Sprintf("• *Repository:* %s\n", state.TargetRepo))
		}
	case "dns":
		switch state.ActionType {
		case "add":
			sb.WriteString(fmt.Sprintf("• *Zone:* %s\n", state.TargetZone))
			sb.WriteString(fmt.Sprintf("• *Record:* %s (%s) -> %s\n", state.DnsConfig.Name, state.DnsConfig.Type, state.DnsConfig.Content))
		case "delete":
			sb.WriteString(fmt.Sprintf("• *Zone:* %s\n", state.TargetZone))
			sb.WriteString(fmt.Sprintf("• *Record:* %s\n", state.TargetRecord))
		case "settings":
			sb.WriteString(fmt.Sprintf("• *Zone:* %s\n", state.TargetZone))
			sb.WriteString(fmt.Sprintf("• *Record:* %s\n", state.TargetRecord))
		}
	case "org_settings":
		sb.WriteString("- *Resource:* Organization settings\n")
	case "user_management":
		sb.WriteString(fmt.Sprintf("• *Team:* %s\n", state.TeamMemberConfig.Team))
		sb.WriteString(fmt.Sprintf("• *Member:* %s\n", state.TeamMemberConfig.Username))
		if state.ActionType != "delete" {
			sb.WriteString(fmt.Sprintf("• *Role:* %s\n", state.TeamMemberConfig.Role))
		}
	}
}

// buildRequestSummary creates the blocks posted to #opsy-requests.
func buildRequestSummary(state *conversation.State, prTitle, prURL string, linearIssue *linearclient.Issue) []slack.Block {
	var sb strings.Builder
	now := time.Now().UTC().Format("2 Jan 2006, 15:04 UTC")
	sb.WriteString(fmt.Sprintf("• *Request:* %s\n", prTitle))
	sb.WriteString(fmt.Sprintf("• *Requested by:* <@%s>\n", state.UserID))
	sb.WriteString(fmt.Sprintf("• *Requested at:* %s\n", now))
	sb.WriteString(fmt.Sprintf("• *Priority:* %s %s\n", priorityEmoji(state.Priority), capitalizeFirst(state.Priority)))

	writeBulletResourceDetails(&sb, state)

	if state.Justification != "" {
		sb.WriteString(fmt.Sprintf("• *Justification:* %s\n", state.Justification))
	}

	prLabel := "View PR"
	if matches := prURLPattern.FindStringSubmatch(prURL); len(matches) >= 2 {
		prLabel = "#" + matches[1]
	}
	sb.WriteString(fmt.Sprintf("• *PR:* <%s|%s>\n", prURL, prLabel))
	if linearIssue != nil {
		sb.WriteString(fmt.Sprintf("• *Linear:* <%s|%s>\n", linearIssue.URL, linearIssue.Identifier))
	}
	sb.WriteString("\n:arrow_right: *Action required* — A manager has to approve reacting to this message with :thumbsup:")

	section := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", sb.String(), false, false),
		nil, nil,
	)
	return []slack.Block{section}
}

// buildLinearIssueDescription creates markdown for the Linear issue body.
func buildLinearIssueDescription(state *conversation.State, requesterName, prURL string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Requested by: %s\n", requesterName))

	switch state.ResourceType {
	case "repo":
		switch state.ActionType {
		case "add":
			sb.WriteString(fmt.Sprintf("Repository: %s\n", state.RepoConfig.Name))
			if state.RepoConfig.Description != "" {
				sb.WriteString(fmt.Sprintf("Description: %s\n", state.RepoConfig.Description))
			}
			sb.WriteString(fmt.Sprintf("Visibility: %s\n", state.RepoConfig.Visibility))
		case "delete":
			sb.WriteString(fmt.Sprintf("Repository: %s\n", state.TargetRepo))
		case "settings":
			sb.WriteString(fmt.Sprintf("Repository: %s\n", state.TargetRepo))
		}
	case "dns":
		sb.WriteString(fmt.Sprintf("Zone: %s\n", state.TargetZone))
		switch state.ActionType {
		case "add":
			sb.WriteString(fmt.Sprintf("Record: %s (%s) → %s\n", state.DnsConfig.Name, state.DnsConfig.Type, state.DnsConfig.Content))
		default:
			sb.WriteString(fmt.Sprintf("Record: %s\n", state.TargetRecord))
		}
	case "org_settings":
		sb.WriteString("Resource: Organization settings\n")
	case "user_management":
		sb.WriteString(fmt.Sprintf("Team: %s\n", state.TeamMemberConfig.Team))
		sb.WriteString(fmt.Sprintf("Member: %s\n", state.TeamMemberConfig.Username))
		if state.ActionType != "delete" {
			sb.WriteString(fmt.Sprintf("Role: %s\n", state.TeamMemberConfig.Role))
		}
	}

	if state.Justification != "" {
		sb.WriteString(fmt.Sprintf("Justification: %s\n", state.Justification))
	}

	sb.WriteString(fmt.Sprintf("PR: %s\n", prURL))
	sb.WriteString("\nCreated by Opsy\n")
	return sb.String()
}

func (h *Handler) fetchTeamNames() []string {
	ctx := context.Background()
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

func (h *Handler) fetchMemberNames() []string {
	ctx := context.Background()
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

func (h *Handler) handleUserManagementAction(state *conversation.State, actionType, triggerID string) {
	teams := h.fetchTeamNames()
	if len(teams) == 0 {
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("No teams found.", false))
		h.store.Delete(state.ThreadTS)
		return
	}
	members := h.fetchMemberNames()
	if len(members) == 0 {
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText("No org members found.", false))
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
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionBlocks(ComingSoonBlocks(actionType)...))
		h.store.Delete(state.ThreadTS)
		return
	}

	if _, err := h.api.OpenView(triggerID, modal); err != nil {
		h.logger.Error("failed to open team member modal", "error", err, "action", actionType)
		h.reply(state, conversation.MsgProgress, "", slack.MsgOptionText(fmt.Sprintf("Failed to open modal: %v", err), false))
	}
}

func (h *Handler) createTeamMemberPR(state *conversation.State) {
	ctx := context.Background()
	cfg := state.TeamMemberConfig

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubMembers)
	if err != nil {
		h.reportError(state, "fetch members HCL", err)
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
		h.reportError(state, "unknown action", fmt.Errorf("unsupported action: %s", state.ActionType))
		return
	}
	if err != nil {
		h.reportError(state, "modify HCL", err)
		return
	}

	branch := ghclient.MemberBranchName(state.ActionType, cfg.Team, cfg.Username)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		h.reportError(state, "create branch", err)
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
		h.reportError(state, "commit file", err)
		return
	}

	requester := h.resolveRequester(state)
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
		h.reportError(state, "create PR", err)
		return
	}

	h.replyPR(state, prTitle, prURL)
}

func (h *Handler) createPR(state *conversation.State) {
	ctx := context.Background()
	repo := state.RepoConfig

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	if err != nil {
		h.reportError(state, "fetch repos HCL", err)
		return
	}

	modified, err := hcleditor.AddRepo(src, repo)
	if err != nil {
		h.reportError(state, "modify HCL", err)
		return
	}

	branch := ghclient.BranchName(repo.Name)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		h.reportError(state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(github): add repository %s", repo.Name)
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubRepos, modified, fileSHA, commitMsg); err != nil {
		h.reportError(state, "commit file", err)
		return
	}

	requester := state.UserID
	if user, err := h.api.GetUserInfo(state.UserID); err != nil {
		h.logger.Error("failed to resolve slack user name", "error", err, "user_id", state.UserID)
	} else {
		requester = user.RealName
	}

	prTitle := "Request: Add GitHub repository"
	prBody := ghclient.BuildPRDescription(repo.Name, repo.Description, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		h.reportError(state, "create PR", err)
		return
	}

	h.replyPR(state, prTitle, prURL)
}

func (h *Handler) createDeletePR(state *conversation.State) {
	ctx := context.Background()
	repoName := state.TargetRepo

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	if err != nil {
		h.reportError(state, "fetch repos HCL", err)
		return
	}

	modified, err := hcleditor.RemoveRepo(src, repoName)
	if err != nil {
		h.reportError(state, "modify HCL", err)
		return
	}

	branch := ghclient.DeleteBranchName(repoName)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		h.reportError(state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(github): remove repository %s", repoName)
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubRepos, modified, fileSHA, commitMsg); err != nil {
		h.reportError(state, "commit file", err)
		return
	}

	requester := state.UserID
	if user, err := h.api.GetUserInfo(state.UserID); err != nil {
		h.logger.Error("failed to resolve slack user name", "error", err, "user_id", state.UserID)
	} else {
		requester = user.RealName
	}

	prTitle := "Request: Remove GitHub repository"
	prBody := ghclient.BuildDeletePRDescription(repoName, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		h.reportError(state, "create PR", err)
		return
	}

	h.replyPR(state, prTitle, prURL)
}

func (h *Handler) fetchRepoNames() []string {
	ctx := context.Background()
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

// checkRepoAlreadyExists returns an error message if a repo with the given name
// already exists in the terraform config.
func (h *Handler) checkRepoAlreadyExists(name string) string {
	names := h.fetchRepoNames()
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return fmt.Sprintf("Repository %q already exists.", n)
		}
	}
	return ""
}

// checkRepoStillExists returns an error message if the repo no longer exists
// in the terraform config.
func (h *Handler) checkRepoStillExists(name string) string {
	names := h.fetchRepoNames()
	for _, n := range names {
		if n == name {
			return ""
		}
	}
	return fmt.Sprintf("Repository %q no longer exists. It may have been removed already.", name)
}

func (h *Handler) fetchHCLSource() ([]byte, error) {
	ctx := context.Background()
	src, _, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	return src, err
}

func (h *Handler) fetchOrgHCLSource() ([]byte, error) {
	ctx := context.Background()
	src, _, err := h.gh.GetFileContent(ctx, pathGitHubOrg)
	return src, err
}

func (h *Handler) createSettingsPR(state *conversation.State) {
	ctx := context.Background()
	repoName := state.TargetRepo

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubRepos)
	if err != nil {
		h.reportError(state, "fetch repos HCL", err)
		return
	}

	modified, err := hcleditor.UpdateRepo(src, repoName, state.RepoConfig)
	if err != nil {
		h.reportError(state, "modify HCL", err)
		return
	}

	branch := ghclient.SettingsBranchName(repoName)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		h.reportError(state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(github): update repository settings for %s", repoName)
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubRepos, modified, fileSHA, commitMsg); err != nil {
		h.reportError(state, "commit file", err)
		return
	}

	requester := state.UserID
	if user, err := h.api.GetUserInfo(state.UserID); err != nil {
		h.logger.Error("failed to resolve slack user name", "error", err, "user_id", state.UserID)
	} else {
		requester = user.RealName
	}

	prTitle := "Request: Update GitHub repository settings"
	prBody := ghclient.BuildSettingsPRDescription(repoName, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		h.reportError(state, "create PR", err)
		return
	}

	h.replyPR(state, prTitle, prURL)
}

// --- DNS PR creation ---

func (h *Handler) createDnsAddPR(state *conversation.State) {
	ctx := context.Background()
	zone := state.TargetZone
	cfg := state.DnsConfig

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathCloudflareDNS)
	if err != nil {
		h.reportError(state, "fetch DNS HCL", err)
		return
	}

	existingKeys, err := hcleditor.ExistingDnsRecordKeys(src, zone)
	if err != nil {
		h.reportError(state, "read existing DNS keys", err)
		return
	}
	cfg.RecordKey = generateDnsRecordKey(cfg.Name, cfg.Type, existingKeys)

	modified, err := hcleditor.AddDnsRecord(src, zone, cfg)
	if err != nil {
		h.reportError(state, "modify HCL", err)
		return
	}

	branch := ghclient.DnsBranchName("add", cfg.RecordKey)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		h.reportError(state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(cloudflare): add DNS record %s", cfg.RecordKey)
	if err := h.gh.UpdateFile(ctx, branch, pathCloudflareDNS, modified, fileSHA, commitMsg); err != nil {
		h.reportError(state, "commit file", err)
		return
	}

	requester := h.resolveRequester(state)
	prTitle := "Request: Add DNS record"
	prBody := ghclient.BuildDnsPRDescription("add", zone, cfg.RecordKey, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		h.reportError(state, "create PR", err)
		return
	}

	h.replyPR(state, prTitle, prURL)
}

func (h *Handler) createDnsRemovePR(state *conversation.State) {
	ctx := context.Background()
	zone := state.TargetZone
	recordKey := state.TargetRecord

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathCloudflareDNS)
	if err != nil {
		h.reportError(state, "fetch DNS HCL", err)
		return
	}

	modified, err := hcleditor.RemoveDnsRecord(src, zone, recordKey)
	if err != nil {
		h.reportError(state, "modify HCL", err)
		return
	}

	branch := ghclient.DnsBranchName("delete", recordKey)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		h.reportError(state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(cloudflare): remove DNS record %s", recordKey)
	if err := h.gh.UpdateFile(ctx, branch, pathCloudflareDNS, modified, fileSHA, commitMsg); err != nil {
		h.reportError(state, "commit file", err)
		return
	}

	requester := h.resolveRequester(state)
	prTitle := "Request: Remove DNS record"
	prBody := ghclient.BuildDnsPRDescription("delete", zone, recordKey, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		h.reportError(state, "create PR", err)
		return
	}

	h.replyPR(state, prTitle, prURL)
}

func (h *Handler) createDnsUpdatePR(state *conversation.State) {
	ctx := context.Background()
	zone := state.TargetZone
	recordKey := state.TargetRecord

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathCloudflareDNS)
	if err != nil {
		h.reportError(state, "fetch DNS HCL", err)
		return
	}

	modified, err := hcleditor.UpdateDnsRecord(src, zone, recordKey, state.DnsConfig)
	if err != nil {
		h.reportError(state, "modify HCL", err)
		return
	}

	branch := ghclient.DnsBranchName("update", recordKey)
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		h.reportError(state, "create branch", err)
		return
	}

	commitMsg := fmt.Sprintf("feat(cloudflare): update DNS record %s", recordKey)
	if err := h.gh.UpdateFile(ctx, branch, pathCloudflareDNS, modified, fileSHA, commitMsg); err != nil {
		h.reportError(state, "commit file", err)
		return
	}

	requester := h.resolveRequester(state)
	prTitle := "Request: Update DNS record"
	prBody := ghclient.BuildDnsPRDescription("settings", zone, recordKey, requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		h.reportError(state, "create PR", err)
		return
	}

	h.replyPR(state, prTitle, prURL)
}

func (h *Handler) createOrgSettingsPR(state *conversation.State) {
	ctx := context.Background()

	src, fileSHA, err := h.gh.GetFileContent(ctx, pathGitHubOrg)
	if err != nil {
		h.reportError(state, "fetch org HCL", err)
		return
	}

	modified, err := hcleditor.UpdateOrgSettings(src, state.OrgConfig)
	if err != nil {
		h.reportError(state, "modify HCL", err)
		return
	}

	branch := ghclient.OrgSettingsBranchName()
	if err := h.gh.CreateBranchFromMain(ctx, branch); err != nil {
		h.reportError(state, "create branch", err)
		return
	}

	commitMsg := "feat(github): update organization settings"
	if err := h.gh.UpdateFile(ctx, branch, pathGitHubOrg, modified, fileSHA, commitMsg); err != nil {
		h.reportError(state, "commit file", err)
		return
	}

	requester := h.resolveRequester(state)
	prTitle := "Request: Update GitHub organization settings"
	prBody := ghclient.BuildOrgSettingsPRDescription(requester, state.Justification)
	prURL, err := h.gh.CreatePR(ctx, branch, prTitle, prBody)
	if err != nil {
		h.reportError(state, "create PR", err)
		return
	}

	h.replyPR(state, prTitle, prURL)
}

func orgConfigEqual(a, b conversation.OrgConfig) bool {
	return a == b
}

func (h *Handler) fetchCloudflareHCL() ([]byte, error) {
	ctx := context.Background()
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

// checkDnsAddConflict fetches live HCL and checks if adding a record with the
// given name and type would conflict with existing records in the zone.
// Returns an error message or empty string if no conflict.
func (h *Handler) checkDnsAddConflict(zone, name, typ string) string {
	src, err := h.fetchCloudflareHCL()
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

// checkDnsRecordStillExists fetches live HCL and verifies the record key still
// exists in the zone. Returns an error message or empty string if found.
func (h *Handler) checkDnsRecordStillExists(zone, key string) string {
	src, err := h.fetchCloudflareHCL()
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

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (h *Handler) resolveRequester(state *conversation.State) string {
	requester := state.UserID
	if user, err := h.api.GetUserInfo(state.UserID); err != nil {
		h.logger.Error("failed to resolve slack user name", "error", err, "user_id", state.UserID)
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

func (h *Handler) reportError(state *conversation.State, step string, err error) {
	h.logger.Error("PR creation failed", "step", step, "error", err, "user", state.UserID)
	h.api.PostMessage(state.ChannelID,
		slack.MsgOptionText(fmt.Sprintf("Something went wrong at %s: %v", step, err), false),
		slack.MsgOptionTS(state.ThreadTS))

	current := h.store.Get(state.ThreadTS)
	if current != nil && current.Nonce == state.Nonce {
		h.store.Delete(state.ThreadTS)
	}
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
		h.api.UpdateMessage(state.ChannelID, msg.TS, slack.MsgOptionBlocks(blocks...))
	}
}
