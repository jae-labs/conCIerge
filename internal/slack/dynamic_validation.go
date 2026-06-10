package slack

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jae-labs/concierge/internal/schema"
	"github.com/slack-go/slack"
)

// ParseDynamicSubmission extracts submitted field values for one step.
// Unknown field types are silently skipped.
func ParseDynamicSubmission(
	values map[string]map[string]slack.BlockAction,
	step schema.Step,
	dynamicKeys map[string][]string,
) map[string]any {
	result := make(map[string]any)
	for _, field := range step.Fields {
		if parsed, ok := parseFieldValue(values, field, dynamicKeys); ok {
			result[field.Path] = parsed
		}
	}
	return result
}

func parseFieldValue(values map[string]map[string]slack.BlockAction, field schema.Field, dynamicKeys map[string][]string) (any, bool) {
	switch field.Type {
	case schema.TypeString, schema.TypeSelect:
		if action, ok := fieldAction(values, field.Path); ok {
			if field.Type == schema.TypeSelect {
				v := action.SelectedOption.Value
				return v, v != ""
			}
			return action.Value, action.Value != ""
		}
	case schema.TypeInteger:
		if v, ok := stringField(values, field.Path); ok {
			if i, err := strconv.Atoi(v); err == nil {
				return i, true
			}
			return v, true
		}
	case schema.TypeNumber:
		if v, ok := stringField(values, field.Path); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f, true
			}
			return v, true
		}
	case schema.TypeBoolean:
		if action, ok := fieldAction(values, field.Path); ok {
			return len(action.SelectedOptions) > 0, true
		}
	case schema.TypeListString:
		if v, ok := stringField(values, field.Path); ok {
			return splitCSV(v), true
		}
	case schema.TypeListInteger:
		if v, ok := stringField(values, field.Path); ok {
			ints := make([]int, 0)
			for _, part := range splitCSV(v) {
				if i, err := strconv.Atoi(part); err == nil {
					ints = append(ints, i)
				}
			}
			return ints, true
		}
	case schema.TypeMapString:
		mapVal := collectMapValues(values, field.Path, dynamicKeys[field.Path])
		return mapVal, true
	}
	return nil, false
}

// ValidateDynamicSubmission returns a map[blockID]errorMessage suitable for a
// Slack errors response. Empty map means no errors.
func ValidateDynamicSubmission(
	values map[string]map[string]slack.BlockAction,
	step schema.Step,
	dynamicKeys map[string][]string,
) map[string]string {
	errors := make(map[string]string)
	for _, field := range step.Fields {
		if msg := validateField(values, field, dynamicKeys); msg != "" {
			errors[fieldBlockID(field.Path)] = msg
		}
	}
	return errors
}

func validateField(values map[string]map[string]slack.BlockAction, field schema.Field, dynamicKeys map[string][]string) string {
	switch field.Type {
	case schema.TypeString, schema.TypeSelect, schema.TypeListString:
		val := ""
		if action, ok := fieldAction(values, field.Path); ok {
			if field.Type == schema.TypeSelect {
				val = action.SelectedOption.Value
			} else {
				val = action.Value
			}
		}
		if field.Required && val == "" {
			return field.Label + " is required."
		}
	case schema.TypeInteger:
		v, _ := stringField(values, field.Path)
		if field.Required && v == "" {
			return field.Label + " is required."
		}
		if v != "" {
			if _, err := strconv.Atoi(v); err != nil {
				return field.Label + " must be a valid integer."
			}
		}
	case schema.TypeNumber:
		v, _ := stringField(values, field.Path)
		if field.Required && v == "" {
			return field.Label + " is required."
		}
		if v != "" {
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				return field.Label + " must be a valid number."
			}
		}
	case schema.TypeListInteger:
		v, _ := stringField(values, field.Path)
		if field.Required && v == "" {
			return field.Label + " is required."
		}
		if v != "" {
			for _, part := range splitCSV(v) {
				if _, err := strconv.Atoi(part); err != nil {
					return field.Label + " must be a list of valid integers."
				}
			}
		}
	case schema.TypeBoolean:
		checked := false
		if action, ok := fieldAction(values, field.Path); ok {
			checked = len(action.SelectedOptions) > 0
		}
		if field.Required && !checked {
			return field.Label + " must be checked."
		}
	case schema.TypeMapString:
		if field.Required && !anyMapSelection(values, field.Path, dynamicKeys[field.Path]) {
			return fmt.Sprintf("At least one %s must be assigned.", field.Label)
		}
	}
	return ""
}

func fieldAction(values map[string]map[string]slack.BlockAction, path string) (slack.BlockAction, bool) {
	block, ok := values[fieldBlockID(path)]
	if !ok {
		return slack.BlockAction{}, false
	}
	action, ok := block[fieldElemID(path)]
	return action, ok
}

func stringField(values map[string]map[string]slack.BlockAction, path string) (string, bool) {
	action, ok := fieldAction(values, path)
	if !ok || action.Value == "" {
		return action.Value, ok && action.Value != ""
	}
	return action.Value, true
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func collectMapValues(values map[string]map[string]slack.BlockAction, path string, keys []string) map[string]string {
	out := make(map[string]string)
	for _, k := range keys {
		block, ok := values[mapEntryBlockID(path, k)]
		if !ok {
			continue
		}
		action, ok := block[mapEntryElemID(path, k)]
		if !ok {
			continue
		}
		val := action.SelectedOption.Value
		if val != "" && val != noneOptionValue {
			out[k] = val
		}
	}
	return out
}

func anyMapSelection(values map[string]map[string]slack.BlockAction, path string, keys []string) bool {
	for _, k := range keys {
		block, ok := values[mapEntryBlockID(path, k)]
		if !ok {
			continue
		}
		action, ok := block[mapEntryElemID(path, k)]
		if !ok {
			continue
		}
		val := action.SelectedOption.Value
		if val != "" && val != noneOptionValue {
			return true
		}
	}
	return false
}
