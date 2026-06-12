package slack

import (
	"fmt"
	"strings"

	"github.com/jae-labs/concierge/internal/schema"
	"github.com/slack-go/slack"
)

const (
	slackModalTitleMax = 24
	noneOptionValue    = "none"
)

func clampSlackModalTitle(title string) string {
	if len(title) <= slackModalTitleMax {
		return title
	}
	if slackModalTitleMax <= 3 {
		return title[:slackModalTitleMax]
	}
	return strings.TrimSpace(title[:slackModalTitleMax-3]) + "..."
}

// BuildDynamicModal renders one wizard step into a Slack Block Kit modal. The
// callback ID tells Slack which step submission handler to invoke; the
// metadata round-trips the thread+nonce identity.
func BuildDynamicModal(
	resource *schema.Resource,
	stepIndex int,
	existingValues map[string]any,
	dynamicKeys map[string][]string, // field path -> available keys for select/map_string fields
	callbackID string,
	metadata string,
) slack.ModalViewRequest {
	step := resource.Steps[stepIndex]
	blocks := make([]slack.Block, 0, len(step.Fields)+2)

	isCreate := isCreateCallback(callbackID)
	if stepIndex == 0 && isCreate && resource.Kind == schema.KindMapEntry {
		blocks = append(blocks, newResourceKeyInputBlock(resource.KeyLabel))
	}

	for _, field := range step.Fields {
		blocks = append(blocks, renderFieldBlocks(field, existingValues[field.Path], dynamicKeys)...)
	}

	submitText := "Next"
	if stepIndex == len(resource.Steps)-1 {
		submitText = "Confirm"
		if isCreate || resource.Kind == schema.KindSingleton {
			blocks = append(blocks, newJustificationBlock())
		}
	}
	titleText := clampSlackModalTitle(fmt.Sprintf("%s (%d/%d)", resource.Label, stepIndex+1, len(resource.Steps)))

	return slack.ModalViewRequest{
		Type:            slack.VTModal,
		Title:           slack.NewTextBlockObject("plain_text", titleText, false, false),
		Submit:          slack.NewTextBlockObject("plain_text", submitText, false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		CallbackID:      callbackID,
		PrivateMetadata: metadata,
		Blocks:          slack.Blocks{BlockSet: blocks},
	}
}

// BuildDynamicSelectModal renders the "pick the target resource to edit/delete"
// step for map_entry update and delete flows.
func BuildDynamicSelectModal(
	resource *schema.Resource,
	actionType string, // "settings" or "delete"
	existingKeys []string,
	callbackID string,
	metadata string,
) slack.ModalViewRequest {
	selBlock := newKeySelectBlock(resource.KeyLabel, existingKeys)
	justBlock := newJustificationBlock()

	submitLabel := "Next"
	if actionType == schema.ActionDelete {
		submitLabel = "Confirm"
	}
	titleText := "Edit " + resource.Label
	if actionType == schema.ActionDelete {
		titleText = "Remove " + resource.Label
	}

	return slack.ModalViewRequest{
		Type:            slack.VTModal,
		Title:           slack.NewTextBlockObject("plain_text", clampSlackModalTitle(titleText), false, false),
		Submit:          slack.NewTextBlockObject("plain_text", submitLabel, false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		CallbackID:      callbackID,
		PrivateMetadata: metadata,
		Blocks:          slack.Blocks{BlockSet: []slack.Block{selBlock, justBlock}},
	}
}

// isCreateCallback returns true for any dynamic_create_step_N callback ID.
func isCreateCallback(id string) bool {
	parsed, ok := parseDynamicCallback(id)
	return ok && parsed.Mode == flowCreate
}

func newResourceKeyInputBlock(label string) slack.Block {
	keyElem := slack.NewPlainTextInputBlockElement(
		slack.NewTextBlockObject("plain_text", "e.g. my-resource-name", false, false),
		ElemResourceKey,
	)
	return slack.NewInputBlock(
		BlockResourceKey,
		slack.NewTextBlockObject("plain_text", label, false, false),
		nil, keyElem,
	)
}

func newKeySelectBlock(label string, existingKeys []string) slack.Block {
	opts := make([]*slack.OptionBlockObject, 0, len(existingKeys))
	for _, k := range existingKeys {
		opts = append(opts, slack.NewOptionBlockObject(k,
			slack.NewTextBlockObject("plain_text", k, false, false), nil))
	}
	selElem := slack.NewOptionsSelectBlockElement("static_select",
		slack.NewTextBlockObject("plain_text", "Select a target...", false, false),
		ElemResourceKey, opts...,
	)
	return slack.NewInputBlock(BlockResourceKey,
		slack.NewTextBlockObject("plain_text", label, false, false), nil, selElem)
}

func newJustificationBlock() slack.Block {
	justElem := slack.NewPlainTextInputBlockElement(
		slack.NewTextBlockObject("plain_text", "Why is this change being requested?", false, false),
		ElemJustification)
	justElem.WithMultiline(true)
	justElem.WithMinLength(20)
	return slack.NewInputBlock(BlockJustification,
		slack.NewTextBlockObject("plain_text", "Justification", false, false),
		slack.NewTextBlockObject("plain_text", "Minimum 20 characters. This will appear in the PR description.", false, false),
		justElem)
}

// renderFieldBlocks returns the blocks for one schema field. The blocks list
// can be empty (no field type matched) but is never nil.
func renderFieldBlocks(field schema.Field, val any, dynamicKeys map[string][]string) []slack.Block {
	switch field.Type {
	case schema.TypeString, schema.TypeInteger, schema.TypeNumber, schema.TypeListString, schema.TypeListInteger:
		return []slack.Block{renderTextInput(field, val)}
	case schema.TypeBoolean:
		return []slack.Block{renderBooleanInput(field, val)}
	case schema.TypeSelect:
		return []slack.Block{renderSelectInput(field, val, dynamicKeys[field.Path])}
	case schema.TypeMapString:
		return renderMapStringInputs(field, val, dynamicKeys[field.Path])
	}
	return nil
}

func renderTextInput(field schema.Field, val any) slack.Block {
	textElem := slack.NewPlainTextInputBlockElement(
		slack.NewTextBlockObject("plain_text", "Enter "+field.Label, false, false),
		fieldElemID(field.Path),
	)
	if field.Type == schema.TypeListString || field.Type == schema.TypeListInteger {
		textElem.Multiline = true
	}
	textElem.InitialValue = formatInitialValue(val)

	block := slack.NewInputBlock(
		fieldBlockID(field.Path),
		slack.NewTextBlockObject("plain_text", field.Label, false, false),
		nil, textElem,
	)
	block.Optional = !field.Required
	return block
}

func formatInitialValue(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case []string:
		return strings.Join(v, ", ")
	case []int:
		parts := make([]string, 0, len(v))
		for _, i := range v {
			parts = append(parts, fmt.Sprintf("%v", i))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func renderBooleanInput(field schema.Field, val any) slack.Block {
	opt := slack.NewOptionBlockObject("true",
		slack.NewTextBlockObject("plain_text", field.Label, false, false), nil)
	checkboxElem := slack.NewCheckboxGroupsBlockElement(fieldElemID(field.Path), opt)
	if b, ok := val.(bool); ok && b {
		checkboxElem.InitialOptions = []*slack.OptionBlockObject{opt}
	}
	block := slack.NewInputBlock(
		fieldBlockID(field.Path),
		slack.NewTextBlockObject("plain_text", field.Label, false, false),
		nil, checkboxElem,
	)
	block.Optional = true
	return block
}

func renderSelectInput(field schema.Field, val any, dynamic []string) slack.Block {
	optionValues := field.Options
	if len(optionValues) == 0 {
		optionValues = dynamic
	}
	opts := make([]*slack.OptionBlockObject, 0, len(optionValues))
	for _, optVal := range optionValues {
		opts = append(opts, slack.NewOptionBlockObject(optVal,
			slack.NewTextBlockObject("plain_text", optVal, false, false), nil))
	}
	selectElem := slack.NewOptionsSelectBlockElement("static_select",
		slack.NewTextBlockObject("plain_text", "Select...", false, false),
		fieldElemID(field.Path), opts...,
	)
	if initial := pickInitialSelectValue(val, field.Default); initial != "" {
		for _, opt := range opts {
			if opt.Value == initial {
				selectElem.InitialOption = opt
				break
			}
		}
	}
	block := slack.NewInputBlock(
		fieldBlockID(field.Path),
		slack.NewTextBlockObject("plain_text", field.Label, false, false),
		nil, selectElem,
	)
	block.Optional = !field.Required
	return block
}

func pickInitialSelectValue(val, defaultVal any) string {
	if val != nil {
		return fmt.Sprintf("%v", val)
	}
	if defaultVal != nil {
		return fmt.Sprintf("%v", defaultVal)
	}
	return ""
}

func renderMapStringInputs(field schema.Field, val any, keys []string) []slack.Block {
	cfg, _ := val.(map[string]string)

	blocks := make([]slack.Block, 0, len(keys)+1)
	blocks = append(blocks, slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", "*"+field.Label+"*", false, false),
		nil, nil,
	))

	valOptions := buildMapValueOptions(field.ValueOptions)

	for _, k := range keys {
		selectElem := slack.NewOptionsSelectBlockElement("static_select",
			slack.NewTextBlockObject("plain_text", "Select...", false, false),
			mapEntryElemID(field.Path, k), valOptions...,
		)
		initial := noneOptionValue
		if cfg != nil {
			if role, ok := cfg[k]; ok {
				initial = role
			}
		}
		for _, opt := range valOptions {
			if opt.Value == initial {
				selectElem.InitialOption = opt
				break
			}
		}
		blocks = append(blocks, slack.NewInputBlock(
			mapEntryBlockID(field.Path, k),
			slack.NewTextBlockObject("plain_text", k, false, false),
			nil, selectElem,
		))
	}
	return blocks
}

func buildMapValueOptions(extra []string) []*slack.OptionBlockObject {
	opts := make([]*slack.OptionBlockObject, 0, len(extra)+1)
	opts = append(opts, slack.NewOptionBlockObject(noneOptionValue,
		slack.NewTextBlockObject("plain_text", "None / Unassigned", false, false), nil))
	for _, optVal := range extra {
		opts = append(opts, slack.NewOptionBlockObject(optVal,
			slack.NewTextBlockObject("plain_text", optVal, false, false), nil))
	}
	return opts
}
