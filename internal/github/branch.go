package github

import (
	"regexp"
	"strings"
)

var branchSanitizer = regexp.MustCompile(`[^a-z0-9-_]+`)

// SanitizeBranchSegment lowercases input and replaces any non [a-z0-9-_] run
// with a single hyphen, trimming leading/trailing hyphens.
func SanitizeBranchSegment(s string) string {
	s = strings.ToLower(s)
	s = branchSanitizer.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
