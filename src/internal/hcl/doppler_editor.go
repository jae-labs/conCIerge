package hcl

import (
	"bytes"
	"fmt"
	"sort"
	"text/template"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/jae-labs/conCIerge/internal/conversation"
)

// ExistingProjectNames returns project names from the projects map in locals.
func ExistingProjectNames(src []byte) ([]string, error) {
	localsBody, err := localsBlockBody(src)
	if err != nil {
		return nil, err
	}

	projectsAttr, ok := localsBody.Attributes["projects"]
	if !ok {
		return nil, fmt.Errorf("projects attribute not found in locals block")
	}

	objExpr, ok := projectsAttr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, fmt.Errorf("projects is not an object expression")
	}

	var names []string
	for _, item := range objExpr.Items {
		name, err := exprToString(item.KeyExpr)
		if err != nil {
			return nil, fmt.Errorf("read project name: %w", err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ExtractProjectConfig reads a Doppler project from HCL source.
func ExtractProjectConfig(src []byte, projectName string) (conversation.DopplerProjectConfig, error) {
	obj, err := findProjectObject(src, projectName)
	if err != nil {
		return conversation.DopplerProjectConfig{}, err
	}

	cfg := conversation.DopplerProjectConfig{Name: projectName}

	for _, item := range obj.Items {
		fieldName, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		if fieldName == "description" {
			cfg.Description, _ = exprToString(item.ValueExpr)
		}
	}

	return cfg, nil
}

// AddProject inserts a new Doppler project entry into the projects map.
func AddProject(src []byte, cfg conversation.DopplerProjectConfig) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}

	existing, err := ExistingProjectNames(src)
	if err != nil {
		return nil, fmt.Errorf("read existing projects: %w", err)
	}
	for _, name := range existing {
		if name == cfg.Name {
			return nil, fmt.Errorf("project %q already exists", cfg.Name)
		}
	}

	entry, err := renderProjectEntry(cfg)
	if err != nil {
		return nil, fmt.Errorf("render project entry: %w", err)
	}

	offset, err := findProjectsClosingBrace(src)
	if err != nil {
		return nil, fmt.Errorf("find projects closing brace: %w", err)
	}

	var result bytes.Buffer
	result.Write(src[:offset])
	result.WriteString(entry)
	result.Write(src[offset:])

	out := result.Bytes()
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("modified HCL is invalid: %w", err)
	}
	return out, nil
}

// RemoveProject removes a Doppler project entry from the projects map.
func RemoveProject(src []byte, projectName string) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}

	start, end, err := findProjectRange(src, projectName)
	if err != nil {
		return nil, err
	}

	// strip trailing newlines
	for end < len(src) && (src[end] == '\n' || src[end] == '\r') {
		end++
	}

	var result bytes.Buffer
	result.Write(src[:start])
	result.Write(src[end:])
	out := result.Bytes()

	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("modified HCL is invalid: %w", err)
	}
	return out, nil
}

// UpdateProject updates an existing Doppler project's description in the projects map.
func UpdateProject(src []byte, projectName string, cfg conversation.DopplerProjectConfig) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}

	projectObj, err := findProjectObject(src, projectName)
	if err != nil {
		return nil, err
	}

	var descItem *hclsyntax.ObjectConsItem
	for _, item := range projectObj.Items {
		fieldName, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		if fieldName == "description" {
			descItem = &item
			break
		}
	}

	if descItem == nil {
		return nil, fmt.Errorf("description field not found in project %q", projectName)
	}

	start := descItem.ValueExpr.Range().Start.Byte
	end := descItem.ValueExpr.Range().End.Byte

	var result bytes.Buffer
	result.Write(src[:start])
	result.WriteString(fmt.Sprintf("%q", cfg.Description))
	result.Write(src[end:])
	out := result.Bytes()

	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("modified HCL is invalid: %w", err)
	}
	return out, nil
}

// findProjectObject returns the inner ObjectConsExpr for a named project.
func findProjectObject(src []byte, projectName string) (*hclsyntax.ObjectConsExpr, error) {
	localsBody, err := localsBlockBody(src)
	if err != nil {
		return nil, err
	}

	projectsAttr, ok := localsBody.Attributes["projects"]
	if !ok {
		return nil, fmt.Errorf("projects attribute not found in locals block")
	}

	objExpr, ok := projectsAttr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, fmt.Errorf("projects is not an object expression")
	}

	for _, item := range objExpr.Items {
		name, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		if name != projectName {
			continue
		}
		inner, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return nil, fmt.Errorf("project %q value is not an object", projectName)
		}
		return inner, nil
	}
	return nil, fmt.Errorf("project %q not found", projectName)
}

// findProjectRange returns start and end byte offsets for a project entry in HCL.
func findProjectRange(src []byte, projectName string) (int, int, error) {
	localsBody, err := localsBlockBody(src)
	if err != nil {
		return 0, 0, err
	}

	projectsAttr, ok := localsBody.Attributes["projects"]
	if !ok {
		return 0, 0, fmt.Errorf("projects attribute not found in locals block")
	}

	objExpr, ok := projectsAttr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return 0, 0, fmt.Errorf("projects is not an object expression")
	}

	for _, item := range objExpr.Items {
		name, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		if name != projectName {
			continue
		}

		keyStart := item.KeyExpr.Range().Start.Byte
		valEnd := item.ValueExpr.Range().End.Byte

		start := keyStart
		for start > 0 && src[start-1] != '\n' {
			start--
		}

		end := valEnd
		for end < len(src) && src[end] != '\n' {
			end++
		}
		if end < len(src) {
			end++
		}

		return start, end, nil
	}

	return 0, 0, fmt.Errorf("project %q not found", projectName)
}

func findProjectsClosingBrace(src []byte) (int, error) {
	lines := bytes.Split(src, []byte("\n"))
	inProjects := false
	depth := 0
	offset := 0

	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)

		if !inProjects {
			if bytes.Contains(trimmed, []byte("projects")) && bytes.Contains(trimmed, []byte("= {")) {
				inProjects = true
				depth = 1
				offset += len(line) + 1
				continue
			}
		} else {
			for _, b := range line {
				switch b {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						return offset, nil
					}
				}
			}
		}

		offset += len(line) + 1
	}

	return 0, fmt.Errorf("projects closing brace not found")
}

var projectTmpl = template.Must(template.New("project_entry").Parse(`    "{{.Name}}" = {
      description  = "{{.Description}}"
      environments = local.default_envs
    }
`))

func renderProjectEntry(cfg conversation.DopplerProjectConfig) (string, error) {
	var buf bytes.Buffer
	if err := projectTmpl.Execute(&buf, cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
}
