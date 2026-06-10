package hcl

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/jae-labs/concierge/internal/schema"
)

func ReadMembership(src []byte, team, username string) (map[string]any, error) {
	members, maintainers, err := extractTeamRoleLists(src, team)
	if err != nil {
		return nil, err
	}

	for _, member := range maintainers {
		if member == username {
			return map[string]any{"team": team, "username": username, "role": "maintainer"}, nil
		}
	}
	for _, member := range members {
		if member == username {
			return map[string]any{"team": team, "username": username, "role": "member"}, nil
		}
	}

	return nil, fmt.Errorf("%q is not in team %q", username, team)
}

func ApplyMembershipAction(src []byte, values map[string]any, action string, _ *schema.Resource) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}

	team := fmt.Sprintf("%v", values["team"])
	username := fmt.Sprintf("%v", values["username"])
	role := fmt.Sprintf("%v", values["role"])

	switch action {
	case schema.ActionAdd:
		return applyMembershipUpsert(src, team, username, role, false)
	case schema.ActionChangeRole:
		return applyMembershipUpsert(src, team, username, role, true)
	case schema.ActionDelete:
		return applyMembershipDelete(src, team, username)
	default:
		return nil, fmt.Errorf("unsupported membership action %q", action)
	}
}

func extractTeamRoleLists(src []byte, team string) (members []string, maintainers []string, err error) {
	teamObj, err := findTeamObject(src, team)
	if err != nil {
		return nil, nil, err
	}

	for _, item := range teamObj.Items {
		key, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		switch key {
		case "members":
			members, err = extractStringList(item.ValueExpr)
			if err != nil {
				return nil, nil, err
			}
		case "maintainers":
			maintainers, err = extractStringList(item.ValueExpr)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return members, maintainers, nil
}

func findTeamObject(src []byte, team string) (*hclsyntax.ObjectConsExpr, error) {
	teamsObj, err := findRootMap(src, "teams")
	if err != nil {
		return nil, err
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

func extractStringList(expr hclsyntax.Expression) ([]string, error) {
	tuple, ok := expr.(*hclsyntax.TupleConsExpr)
	if !ok {
		return nil, fmt.Errorf("not a tuple expression")
	}
	var items []string
	for _, entry := range tuple.Exprs {
		value, err := exprToString(entry)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, nil
}

func applyMembershipUpsert(src []byte, team, username, role string, requireExisting bool) ([]byte, error) {
	current, err := ReadMembership(src, team, username)
	if err != nil && requireExisting {
		return nil, err
	}

	currentRole, _ := current["role"].(string)
	if currentRole == role {
		return nil, fmt.Errorf("%q is already a %s of %q", username, role, team)
	}

	next := src
	if currentRole != "" {
		next, err = rewriteTeamRoleList(next, team, currentRole, username, false)
		if err != nil {
			return nil, err
		}
	}

	next, err = rewriteTeamRoleList(next, team, role, username, true)
	if err != nil {
		return nil, err
	}

	if _, err := Parse(next); err != nil {
		return nil, fmt.Errorf("invalid output HCL: %w", err)
	}
	return next, nil
}

func applyMembershipDelete(src []byte, team, username string) ([]byte, error) {
	current, err := ReadMembership(src, team, username)
	if err != nil {
		return nil, err
	}

	next, err := rewriteTeamRoleList(src, team, fmt.Sprintf("%v", current["role"]), username, false)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(next); err != nil {
		return nil, fmt.Errorf("invalid output HCL: %w", err)
	}
	return next, nil
}

func rewriteTeamRoleList(src []byte, team, role, username string, add bool) ([]byte, error) {
	attr := role + "s"
	start, end, items, err := findMembershipListRange(src, team, attr)
	if err != nil {
		return nil, err
	}

	var nextItems []string
	found := false
	for _, item := range items {
		if item == username {
			found = true
			if add {
				nextItems = append(nextItems, item)
			}
			continue
		}
		nextItems = append(nextItems, item)
	}

	if add && !found {
		nextItems = append(nextItems, username)
	}
	if !add && !found {
		return nil, fmt.Errorf("%q not found in %s of team %q", username, attr, team)
	}

	sort.Strings(nextItems)

	var replacement string
	if len(nextItems) > 0 {
		quoted := make([]string, 0, len(nextItems))
		for _, item := range nextItems {
			quoted = append(quoted, fmt.Sprintf("%q", item))
		}
		replacement = " " + strings.Join(quoted, ", ") + " "
	}

	var out bytes.Buffer
	out.Write(src[:start])
	out.WriteString(replacement)
	out.Write(src[end:])
	return out.Bytes(), nil
}

func findMembershipListRange(src []byte, team, attr string) (int, int, []string, error) {
	teamObj, err := findTeamObject(src, team)
	if err != nil {
		return 0, 0, nil, err
	}

	for _, item := range teamObj.Items {
		key, err := exprToString(item.KeyExpr)
		if err != nil || key != attr {
			continue
		}
		tuple, ok := item.ValueExpr.(*hclsyntax.TupleConsExpr)
		if !ok {
			return 0, 0, nil, fmt.Errorf("%s is not a list", attr)
		}
		items, err := extractStringList(item.ValueExpr)
		if err != nil {
			return 0, 0, nil, err
		}
		rng := tuple.SrcRange
		return rng.Start.Byte + 1, rng.End.Byte - 1, items, nil
	}

	return 0, 0, nil, fmt.Errorf("%s attribute not found in team %q", attr, team)
}
