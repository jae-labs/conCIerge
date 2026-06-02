package slack

import (
	"regexp"

	goslack "github.com/slack-go/slack"
)

var dopplerNameRe = regexp.MustCompile(`^[a-z0-9-_]+$`)

// validateDopplerAdd checks Doppler project creation modal input values and returns a map of
// block ID to error message. An empty map means all fields are valid.
func validateDopplerAdd(values map[string]map[string]goslack.BlockAction) map[string]string {
	errs := make(map[string]string)

	name := values[BlockDopplerName][ElemDopplerName].Value
	if name == "" {
		errs[BlockDopplerName] = "Project name is required."
	} else if len(name) > 30 {
		errs[BlockDopplerName] = "Project name must be 30 characters or fewer."
	} else if !dopplerNameRe.MatchString(name) {
		errs[BlockDopplerName] = "Must contain only lowercase letters, digits, hyphens, and underscores."
	}

	desc := values[BlockDopplerDesc][ElemDopplerDesc].Value
	if desc == "" {
		errs[BlockDopplerDesc] = "Description is required."
	}

	if justBlock, ok := values[BlockJustification]; ok {
		just := justBlock[ElemJustification].Value
		if len(just) < 20 {
			errs[BlockJustification] = "Justification must be at least 20 characters."
		}
	}

	return errs
}

// validateDopplerUpdate checks Doppler project update modal input values.
func validateDopplerUpdate(values map[string]map[string]goslack.BlockAction) map[string]string {
	errs := make(map[string]string)

	desc := values[BlockDopplerDesc][ElemDopplerDesc].Value
	if desc == "" {
		errs[BlockDopplerDesc] = "Description is required."
	}

	if justBlock, ok := values[BlockJustification]; ok {
		just := justBlock[ElemJustification].Value
		if len(just) < 20 {
			errs[BlockJustification] = "Justification must be at least 20 characters."
		}
	}

	return errs
}
