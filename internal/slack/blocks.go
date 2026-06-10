package slack

import (
	"fmt"

	"github.com/jae-labs/concierge/internal/schema"
	"github.com/slack-go/slack"
)

type CategoryOption struct {
	Value string
	Label string
}

const (
	ActionCategorySelect = "category_select"
	ActionResourceSelect = "resource_select"
	ActionActionSelect   = "action_select"
)

func HomeTabBlocks(userID string) []slack.Block {
	greeting := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Hi <@%s>! I'm *conCierge*, your infrastructure assistant.", userID), false, false),
		nil, nil,
	)
	divider := slack.NewDividerBlock()
	intro := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", "I help your team manage infrastructure from Slack by opening Terraform PRs for review.", false, false),
		nil, nil,
	)
	context := slack.NewContextBlock("home_context",
		slack.NewTextBlockObject("mrkdwn", "Schema-driven resources load from Terraform. If schema is missing, requests cannot start.", false, false),
	)
	return []slack.Block{greeting, divider, intro, divider, context}
}

func WelcomeBlocks(userID string) []slack.Block {
	return []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Hey <@%s>, conCierge is unavailable because schema could not be loaded.", userID), false, false),
			nil, nil,
		),
	}
}

func WelcomeBlocksFromCategories(userID string, categories []CategoryOption) []slack.Block {
	header := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Hey <@%s>, I'm conCierge. Let's get things done.\nWhat would you like to set up?", userID), false, false),
		nil, nil,
	)

	opts := make([]*slack.OptionBlockObject, len(categories))
	for i, c := range categories {
		opts[i] = slack.NewOptionBlockObject(c.Value,
			slack.NewTextBlockObject("plain_text", c.Label, false, false), nil)
	}
	sel := slack.NewOptionsSelectBlockElement("static_select",
		slack.NewTextBlockObject("plain_text", "Select a platform...", false, false),
		ActionCategorySelect, opts...)
	actions := slack.NewActionBlock("welcome_actions", sel)

	return []slack.Block{header, actions}
}

func ResourceBlocksFromOptions(category string, resources []CategoryOption) []slack.Block {
	if len(resources) == 0 {
		return ComingSoonBlocks(category)
	}

	header := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*%s* it is.\nWhat kind of resource are we working with?", category), false, false),
		nil, nil,
	)

	opts := make([]*slack.OptionBlockObject, len(resources))
	for i, r := range resources {
		opts[i] = slack.NewOptionBlockObject(r.Value,
			slack.NewTextBlockObject("plain_text", r.Label, false, false), nil)
	}
	sel := slack.NewOptionsSelectBlockElement("static_select",
		slack.NewTextBlockObject("plain_text", "Select a resource...", false, false),
		ActionResourceSelect, opts...)
	actions := slack.NewActionBlock("resource_actions", sel)

	return []slack.Block{header, actions}
}

func ActionBlocksFromOptions(resource string, actions []CategoryOption) []slack.Block {
	if len(actions) == 0 {
		return ComingSoonBlocks(resource)
	}

	header := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", "Got it. What would you like to do with this resource?", false, false),
		nil, nil,
	)

	opts := make([]*slack.OptionBlockObject, len(actions))
	for i, a := range actions {
		opts[i] = slack.NewOptionBlockObject(a.Value,
			slack.NewTextBlockObject("plain_text", a.Label, false, false), nil)
	}
	sel := slack.NewOptionsSelectBlockElement("static_select",
		slack.NewTextBlockObject("plain_text", "Select an action...", false, false),
		ActionActionSelect, opts...)
	actionsBlock := slack.NewActionBlock("action_actions", sel)

	return []slack.Block{header, actionsBlock}
}

func CategoryOptionsFromSchema(categories []schema.Category) []CategoryOption {
	opts := make([]CategoryOption, 0, len(categories))
	for _, category := range categories {
		opts = append(opts, CategoryOption{Value: category.ID, Label: category.Label})
	}
	return opts
}

func ResourceOptionsFromSchema(resources []schema.Resource) []CategoryOption {
	opts := make([]CategoryOption, 0, len(resources))
	for _, resource := range resources {
		opts = append(opts, CategoryOption{Value: resource.ID, Label: resource.Label})
	}
	return opts
}

func ActionOptionsFromSchema(resource schema.Resource) []CategoryOption {
	opts := make([]CategoryOption, 0, len(resource.Actions))
	for _, action := range resource.Actions {
		label := action
		switch action {
		case schema.ActionAdd:
			label = "Add"
		case schema.ActionDelete:
			label = "Remove"
		case schema.ActionChangeRole:
			label = "Change Role"
		case schema.ActionSettings:
			label = "Update"
		}
		opts = append(opts, CategoryOption{Value: action, Label: label})
	}
	return opts
}

func LockedCategoryBlocks(categoryLabel string) []slack.Block {
	header := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", "What would you like to set up?", false, false),
		nil, nil,
	)
	selected := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("> Platform: *%s*", categoryLabel), false, false),
		nil, nil,
	)
	return []slack.Block{header, selected}
}

func LockedResourceBlocks(category, resourceLabel string) []slack.Block {
	header := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*%s* it is.\nWhat kind of resource are we working with?", category), false, false),
		nil, nil,
	)
	selected := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("> Resource: *%s*", resourceLabel), false, false),
		nil, nil,
	)
	return []slack.Block{header, selected}
}

func LockedActionBlocks(actionLabel string) []slack.Block {
	header := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", "Got it. What would you like to do with this resource?", false, false),
		nil, nil,
	)
	selected := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("> Action: *%s*", actionLabel), false, false),
		nil, nil,
	)
	return []slack.Block{header, selected}
}

func FlowEndedBlocks() []slack.Block {
	text := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", "This flow is no longer active. Open a *New Chat* to start another request.", false, false),
		nil, nil,
	)
	return []slack.Block{text}
}

func LockedConfirmationBlocks() []slack.Block {
	text := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", "Request submitted. This flow is complete.", false, false),
		nil, nil,
	)
	return []slack.Block{text}
}

func labelForValue(options []CategoryOption, value string) string {
	for _, opt := range options {
		if opt.Value == value {
			return opt.Label
		}
	}
	return value
}

func ComingSoonBlocks(resource string) []slack.Block {
	text := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*%s* is not available. Check `concierge-schema.yaml`.", resource), false, false),
		nil, nil,
	)
	return []slack.Block{text}
}
