package github

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9-]+`)

func BranchName(repoName string) string {
	sanitized := strings.ToLower(repoName)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = nonAlphaNum.ReplaceAllString(sanitized, "")
	suffix := time.Now().Format("20060102-150405")
	return fmt.Sprintf("concierge/add-repo-%s-%s", sanitized, suffix)
}

func DeleteBranchName(repoName string) string {
	sanitized := strings.ToLower(repoName)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = nonAlphaNum.ReplaceAllString(sanitized, "")
	suffix := time.Now().Format("20060102-150405")
	return fmt.Sprintf("concierge/delete-repo-%s-%s", sanitized, suffix)
}

func BuildDeletePRDescription(repoName, requester, justification string) string {
	return fmt.Sprintf(`## Remove repository

### Resource
- %s

### Justification
%s

### Requested by
- %s

### Changes
- Removes repository entry from `+"`terraform/github/locals_repos.tf`"+`

---
*Created by conCierge*`, repoName, justification, requester)
}

func SettingsBranchName(repoName string) string {
	sanitized := strings.ToLower(repoName)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = nonAlphaNum.ReplaceAllString(sanitized, "")
	suffix := time.Now().Format("20060102-150405")
	return fmt.Sprintf("concierge/update-repo-%s-%s", sanitized, suffix)
}

func BuildSettingsPRDescription(repoName, requester, justification string) string {
	return fmt.Sprintf(`## Update repository settings

### Resource
- %s

### Justification
%s

### Requested by
- %s

### Changes
- Updates repository settings in `+"`terraform/github/locals_repos.tf`"+`

---
*Created by conCierge*`, repoName, justification, requester)
}

func DnsBranchName(action, recordKey string) string {
	sanitized := strings.ToLower(recordKey)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = nonAlphaNum.ReplaceAllString(sanitized, "")
	suffix := time.Now().Format("20060102-150405")
	return fmt.Sprintf("concierge/%s-dns-%s-%s", action, sanitized, suffix)
}

func BuildDnsPRDescription(action, zone, recordKey, requester, justification string) string {
	var verb string
	switch action {
	case "add":
		verb = "Add"
	case "delete":
		verb = "Remove"
	case "settings":
		verb = "Update"
	default:
		verb = strings.ToUpper(action[:1]) + action[1:]
	}
	return fmt.Sprintf(`## %s DNS record

### Resource
- %s (zone: %s)

### Justification
%s

### Requested by
- %s

### Changes
- %ss DNS record entry in `+"`terraform/cloudflare/locals_dns.tf`"+`

---
*Created by conCierge*`, verb, recordKey, zone, justification, requester, verb)
}

func OrgSettingsBranchName() string {
	suffix := time.Now().Format("20060102-150405")
	return fmt.Sprintf("concierge/update-org-settings-%s", suffix)
}

func BuildOrgSettingsPRDescription(requester, justification string) string {
	return fmt.Sprintf(`## Update organization settings

### Justification
%s

### Requested by
- %s

### Changes
- Updates organization settings in `+"`terraform/github/locals_org.tf`"+`

---
*Created by conCierge*`, justification, requester)
}

func BuildPRDescription(repoName, description, requester, justification string) string {
	return fmt.Sprintf(`## Add repository

### Resource
- %s

### Description
%s

### Justification
%s

### Requested by
- %s

### Changes
- Adds new repository entry to `+"`terraform/github/locals_repos.tf`"+`

---
*Created by conCierge*`, repoName, description, justification, requester)
}

func MemberBranchName(action, team, username string) string {
	// normalize action verbs for user-facing branch names
	verb := action
	switch action {
	case "delete":
		verb = "remove"
	case "change_role":
		verb = "update"
	}
	sanitized := strings.ToLower(team + "-" + username)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = nonAlphaNum.ReplaceAllString(sanitized, "")
	suffix := time.Now().Format("20060102-150405")
	return fmt.Sprintf("concierge/%s-member-%s-%s", verb, sanitized, suffix)
}

func BuildMemberPRDescription(action, team, username, role, requester, justification string) string {
	var verb string
	switch action {
	case "add":
		verb = "Add member to"
	case "remove":
		verb = "Remove member from"
	case "change_role":
		verb = "Change member role in"
	default:
		verb = strings.ToUpper(action[:1]) + action[1:]
	}

	roleLine := ""
	if action != "remove" {
		roleLine = fmt.Sprintf("\n- Role: %s", role)
	}

	return fmt.Sprintf(`## %s team

### Resource
- Team: %s
- Member: %s%s

### Justification
%s

### Requested by
- %s

### Changes
- Modifies team membership in `+"`terraform/github/locals_members.tf`"+`

---
*Created by conCierge*`, verb, team, username, roleLine, justification, requester)
}
