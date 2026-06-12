package slack

import (
	"encoding/json"
	"fmt"

	goslack "github.com/slack-go/slack"
)

const (
	slackModalTextMax       = 24
	slackModalCallbackIDMax = 255
	slackModalMetadataMax   = 3000
	slackBlockArrayMax      = 100
	slackBlockOrActionIDMax = 255
	slackOptionsArrayMax    = 100
	slackOptionTextMax      = 75
	slackOptionValueMax     = 150
)

func validateModalViewRequest(view goslack.ModalViewRequest) error {
	if view.Title != nil && len(view.Title.Text) > slackModalTextMax {
		return fmt.Errorf("title exceeds Slack limit: %d", len(view.Title.Text))
	}
	if view.Submit != nil && len(view.Submit.Text) > slackModalTextMax {
		return fmt.Errorf("submit exceeds Slack limit: %d", len(view.Submit.Text))
	}
	if view.Close != nil && len(view.Close.Text) > slackModalTextMax {
		return fmt.Errorf("close exceeds Slack limit: %d", len(view.Close.Text))
	}
	if len(view.CallbackID) > slackModalCallbackIDMax {
		return fmt.Errorf("callback_id exceeds Slack limit: %d", len(view.CallbackID))
	}
	if len(view.PrivateMetadata) > slackModalMetadataMax {
		return fmt.Errorf("private_metadata exceeds Slack limit: %d", len(view.PrivateMetadata))
	}
	if len(view.Blocks.BlockSet) > slackBlockArrayMax {
		return fmt.Errorf("blocks exceed Slack limit: %d", len(view.Blocks.BlockSet))
	}

	raw, err := json.Marshal(view)
	if err != nil {
		return fmt.Errorf("marshal modal view: %w", err)
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("decode modal view: %w", err)
	}
	return validateSlackIDs(payload)
}

func validateSlackIDs(node any) error {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			if (key == "block_id" || key == "action_id") && len(stringValue(child)) > slackBlockOrActionIDMax {
				return fmt.Errorf("%s exceeds Slack limit: %d", key, len(stringValue(child)))
			}
			if key == "options" {
				if options, ok := child.([]any); ok {
					if len(options) > slackOptionsArrayMax {
						return fmt.Errorf("options exceeds Slack limit: %d", len(options))
					}
					for _, option := range options {
						optMap, ok := option.(map[string]any)
						if !ok {
							continue
						}
						if value := stringValue(optMap["value"]); len(value) > slackOptionValueMax {
							return fmt.Errorf("option value exceeds Slack limit: %d", len(value))
						}
						if textMap, ok := optMap["text"].(map[string]any); ok {
							if text := stringValue(textMap["text"]); len(text) > slackOptionTextMax {
								return fmt.Errorf("option text exceeds Slack limit: %d", len(text))
							}
						}
					}
				}
			}
			if err := validateSlackIDs(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := validateSlackIDs(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
