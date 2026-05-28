package hcl

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// ExtractMemberNames returns sorted usernames from the members map in locals.
func ExtractMemberNames(src []byte) ([]string, error) {
	localsBody, err := localsBlockBody(src)
	if err != nil {
		return nil, err
	}

	membersAttr, ok := localsBody.Attributes["members"]
	if !ok {
		return nil, fmt.Errorf("members attribute not found in locals block")
	}

	objExpr, ok := membersAttr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, fmt.Errorf("members is not an object expression")
	}

	var names []string
	for _, item := range objExpr.Items {
		name, err := exprToString(item.KeyExpr)
		if err != nil {
			return nil, fmt.Errorf("read member name: %w", err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// findTeamObject locates a team's ObjectConsExpr within the teams map.
func findTeamObject(src []byte, team string) (*hclsyntax.ObjectConsExpr, error) {
	localsBody, err := localsBlockBody(src)
	if err != nil {
		return nil, err
	}

	teamsAttr, ok := localsBody.Attributes["teams"]
	if !ok {
		return nil, fmt.Errorf("teams attribute not found in locals block")
	}

	teamsObj, ok := teamsAttr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, fmt.Errorf("teams is not an object expression")
	}

	for _, item := range teamsObj.Items {
		name, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		if name == team {
			obj, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr)
			if !ok {
				return nil, fmt.Errorf("team %q value is not an object", team)
			}
			return obj, nil
		}
	}
	return nil, fmt.Errorf("team %q not found", team)
}

// extractStringList reads a tuple expression as a slice of strings.
func extractStringList(expr hclsyntax.Expression) ([]string, error) {
	tuple, ok := expr.(*hclsyntax.TupleConsExpr)
	if !ok {
		return nil, fmt.Errorf("not a tuple expression")
	}
	var items []string
	for _, e := range tuple.Exprs {
		s, err := exprToString(e)
		if err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, nil
}

// ExtractTeamMembers returns the members and maintainers lists for a team.
func ExtractTeamMembers(src []byte, team string) (members []string, maintainers []string, err error) {
	obj, err := findTeamObject(src, team)
	if err != nil {
		return nil, nil, err
	}

	for _, item := range obj.Items {
		key, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		switch key {
		case "members":
			members, err = extractStringList(item.ValueExpr)
			if err != nil {
				return nil, nil, fmt.Errorf("parse members list: %w", err)
			}
		case "maintainers":
			maintainers, err = extractStringList(item.ValueExpr)
			if err != nil {
				return nil, nil, fmt.Errorf("parse maintainers list: %w", err)
			}
		}
	}
	return members, maintainers, nil
}

// IsTeamMember checks if a username is in a team's members or maintainers list.
// Returns (inTeam, role, error) where role is "member" or "maintainer".
func IsTeamMember(src []byte, team, username string) (bool, string, error) {
	members, maintainers, err := ExtractTeamMembers(src, team)
	if err != nil {
		return false, "", err
	}

	for _, m := range maintainers {
		if m == username {
			return true, "maintainer", nil
		}
	}
	for _, m := range members {
		if m == username {
			return true, "member", nil
		}
	}
	return false, "", nil
}

// findListAttr locates a named attribute (members or maintainers) within a team object
// and returns the start/end byte offsets of the list content (between [ and ]).
func findListAttr(src []byte, team, attr string) (listStart, listEnd int, items []string, err error) {
	obj, err := findTeamObject(src, team)
	if err != nil {
		return 0, 0, nil, err
	}

	for _, item := range obj.Items {
		key, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		if key != attr {
			continue
		}
		tuple, ok := item.ValueExpr.(*hclsyntax.TupleConsExpr)
		if !ok {
			return 0, 0, nil, fmt.Errorf("%s is not a list", attr)
		}
		for _, e := range tuple.Exprs {
			s, err := exprToString(e)
			if err != nil {
				return 0, 0, nil, fmt.Errorf("read list item: %w", err)
			}
			items = append(items, s)
		}
		rng := item.ValueExpr.Range()
		// rng covers the full [ ... ] including brackets
		return rng.Start.Byte + 1, rng.End.Byte - 1, items, nil
	}
	return 0, 0, nil, fmt.Errorf("%s attribute not found in team %q", attr, team)
}

// AddTeamMember adds a username to a team's members or maintainers list.
// If the user is already in the other list, they are moved (role change).
// Returns an error if the user is already in the target list.
func AddTeamMember(src []byte, team, username, role string) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}

	targetAttr := role + "s" // "member" -> "members", "maintainer" -> "maintainers"
	otherAttr := "maintainers"
	if role == "maintainer" {
		otherAttr = "members"
	}

	// check if already in target list
	inTeam, currentRole, err := IsTeamMember(src, team, username)
	if err != nil {
		return nil, err
	}
	if inTeam && currentRole == role {
		return nil, fmt.Errorf("%q is already a %s of %q", username, role, team)
	}

	// if in other list, remove first
	result := src
	if inTeam && currentRole != role {
		result, err = removeFromList(result, team, otherAttr, username)
		if err != nil {
			return nil, fmt.Errorf("remove from %s: %w", otherAttr, err)
		}
	}

	// add to target list
	result, err = addToList(result, team, targetAttr, username)
	if err != nil {
		return nil, fmt.Errorf("add to %s: %w", targetAttr, err)
	}

	if _, err := Parse(result); err != nil {
		return nil, fmt.Errorf("invalid output HCL: %w", err)
	}
	return result, nil
}

// addToList inserts a quoted username into a team's named list attribute.
func addToList(src []byte, team, attr, username string) ([]byte, error) {
	listStart, listEnd, items, err := findListAttr(src, team, attr)
	if err != nil {
		return nil, err
	}

	entry := fmt.Sprintf("%q", username)
	var replacement string
	if len(items) == 0 {
		replacement = entry
	} else {
		// insert after the last item with comma
		replacement = string(src[listStart:listEnd]) + ", " + entry
	}

	out := make([]byte, 0, len(src)+len(entry)+4)
	out = append(out, src[:listStart]...)
	out = append(out, []byte(replacement)...)
	out = append(out, src[listEnd:]...)
	return out, nil
}

// removeFromList removes a quoted username from a team's named list attribute.
func removeFromList(src []byte, team, attr, username string) ([]byte, error) {
	listStart, listEnd, items, err := findListAttr(src, team, attr)
	if err != nil {
		return nil, err
	}

	found := false
	for _, item := range items {
		if item == username {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%q not found in %s of team %q", username, attr, team)
	}

	// rebuild the list without the target username
	var remaining []string
	for _, item := range items {
		if item != username {
			remaining = append(remaining, fmt.Sprintf("%q", item))
		}
	}

	replacement := strings.Join(remaining, ", ")

	out := make([]byte, 0, len(src))
	out = append(out, src[:listStart]...)
	out = append(out, []byte(replacement)...)
	out = append(out, src[listEnd:]...)
	return out, nil
}

// RemoveTeamMember removes a username from a team's members or maintainers list.
func RemoveTeamMember(src []byte, team, username string) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}

	inTeam, role, err := IsTeamMember(src, team, username)
	if err != nil {
		return nil, err
	}
	if !inTeam {
		return nil, fmt.Errorf("%q is not a member of %q", username, team)
	}

	attr := role + "s"
	result, err := removeFromList(src, team, attr, username)
	if err != nil {
		return nil, err
	}

	if _, err := Parse(result); err != nil {
		return nil, fmt.Errorf("invalid output HCL: %w", err)
	}
	return result, nil
}

// UpdateTeamMemberRole moves a username between members and maintainers lists.
func UpdateTeamMemberRole(src []byte, team, username, newRole string) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}

	inTeam, currentRole, err := IsTeamMember(src, team, username)
	if err != nil {
		return nil, err
	}
	if !inTeam {
		return nil, fmt.Errorf("%q is not a member of %q", username, team)
	}
	if currentRole == newRole {
		return nil, fmt.Errorf("%q is already a %s of %q", username, newRole, team)
	}

	oldAttr := currentRole + "s"
	newAttr := newRole + "s"

	result, err := removeFromList(src, team, oldAttr, username)
	if err != nil {
		return nil, fmt.Errorf("remove from %s: %w", oldAttr, err)
	}

	result, err = addToList(result, team, newAttr, username)
	if err != nil {
		return nil, fmt.Errorf("add to %s: %w", newAttr, err)
	}

	if _, err := Parse(result); err != nil {
		return nil, fmt.Errorf("invalid output HCL: %w", err)
	}
	return result, nil
}
