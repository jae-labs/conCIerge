package hcl

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/jae-labs/concierge/internal/schema"
)

// indent budgets used when generating new HCL fragments. Different roots
// (map-entry under "repos = {...}" vs singleton at the locals top level) sit
// at different nesting depths in the source file, so the engine needs two
// flavors that differ only in leading whitespace.
const (
	mapEntryIndent  = "      "
	singletonIndent = "  "
)

// ReadResource extracts schema-shaped values for a single map_entry key.
func ReadResource(src []byte, varName, key string, resource *schema.Resource) (map[string]any, error) {
	rootObj, err := findRootMap(src, varName)
	if err != nil {
		return nil, err
	}
	targetObj, err := findChildObject(rootObj, key, varName)
	if err != nil {
		return nil, err
	}
	return collectValues(targetObj, resource), nil
}

// ReadSingleton extracts schema-shaped values from the root object directly.
func ReadSingleton(src []byte, rootPath string, resource *schema.Resource) (map[string]any, error) {
	rootObj, err := findRootMap(src, rootPath)
	if err != nil {
		return nil, err
	}
	return collectValues(rootObj, resource), nil
}

// collectValues runs extractValue for every field defined in the schema,
// skipping fields that fail to extract.
func collectValues(obj *hclsyntax.ObjectConsExpr, resource *schema.Resource) map[string]any {
	values := make(map[string]any)
	for _, step := range resource.Steps {
		for _, field := range step.Fields {
			path := strings.Split(field.Path, ".")
			val, err := extractValue(obj, path, field.Type)
			if err != nil || val == nil {
				continue
			}
			values[field.Path] = val
		}
	}
	return values
}

func extractValue(obj *hclsyntax.ObjectConsExpr, path []string, fType string) (any, error) {
	if len(path) == 0 || obj == nil {
		return nil, fmt.Errorf("empty path or nil object")
	}
	for _, item := range obj.Items {
		k, err := exprToString(item.KeyExpr)
		if err != nil || k != path[0] {
			continue
		}
		if len(path) == 1 {
			return extractLeaf(item.ValueExpr, fType)
		}
		nestedObj, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			if isNullTraversal(item.ValueExpr) {
				return nil, nil
			}
			return nil, fmt.Errorf("field %q is not a nested object", path[0])
		}
		return extractValue(nestedObj, path[1:], fType)
	}
	return nil, nil
}

// extractLeaf decodes a leaf HCL expression into the Go type that matches the
// schema field type.
func extractLeaf(expr hclsyntax.Expression, fType string) (any, error) {
	switch fType {
	case schema.TypeString, schema.TypeSelect:
		return exprToString(expr)
	case schema.TypeBoolean:
		return exprToBool(expr)
	case schema.TypeInteger:
		return exprToInt(expr)
	case schema.TypeNumber:
		return exprToFloat(expr)
	case schema.TypeListString:
		return extractStringList(expr)
	case schema.TypeListInteger:
		return extractIntList(expr)
	case schema.TypeMapString:
		return extractStringMap(expr)
	}
	return nil, fmt.Errorf("unsupported field type %q", fType)
}

func extractIntList(expr hclsyntax.Expression) ([]int, error) {
	tuple, ok := expr.(*hclsyntax.TupleConsExpr)
	if !ok {
		return nil, fmt.Errorf("expected list tuple expression")
	}
	out := make([]int, 0, len(tuple.Exprs))
	for _, e := range tuple.Exprs {
		if v, err := exprToInt(e); err == nil {
			out = append(out, v)
		}
	}
	return out, nil
}

func extractStringMap(expr hclsyntax.Expression) (map[string]string, error) {
	inner, ok := expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, fmt.Errorf("expected map object expression")
	}
	out := make(map[string]string)
	for _, item := range inner.Items {
		k, err1 := exprToString(item.KeyExpr)
		v, err2 := exprToString(item.ValueExpr)
		if err1 == nil && err2 == nil {
			out[k] = v
		}
	}
	return out, nil
}

func isNullTraversal(expr hclsyntax.Expression) bool {
	traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr)
	return ok && traversal.Traversal.RootName() == "null"
}

// AddResource inserts a new entry into the local map within the locals block.
func AddResource(src []byte, varName, key string, values map[string]any, resource *schema.Resource) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}
	rootObj, err := findRootMap(src, varName)
	if err != nil {
		return nil, err
	}

	offset, err := findObjectClosingBrace(src, rootObj)
	if err != nil {
		return nil, err
	}

	var result bytes.Buffer
	result.Write(src[:offset])
	result.WriteString(renderResourceEntry(key, values, resource))
	result.Write(src[offset:])

	out := result.Bytes()
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("modified HCL is invalid: %w", err)
	}
	return out, nil
}

// UpdateResource edits attributes of an existing map_entry in place.
func UpdateResource(src []byte, varName, key string, values map[string]any, resource *schema.Resource) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}
	rootObj, err := findRootMap(src, varName)
	if err != nil {
		return nil, err
	}
	targetObj, err := findChildObject(rootObj, key, varName)
	if err != nil {
		return nil, err
	}
	return applyUpdates(src, targetObj, values, resource, mapEntryIndent)
}

// UpdateSingleton edits attributes of a singleton root object in place.
func UpdateSingleton(src []byte, rootPath string, values map[string]any, resource *schema.Resource) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}
	targetObj, err := findRootMap(src, rootPath)
	if err != nil {
		return nil, err
	}
	return applyUpdates(src, targetObj, values, resource, singletonIndent)
}

// RemoveResource deletes a key-value entry from the locals map.
func RemoveResource(src []byte, varName, key string) ([]byte, error) {
	if _, err := Parse(src); err != nil {
		return nil, fmt.Errorf("invalid input HCL: %w", err)
	}
	start, end, err := findResourceRange(src, varName, key)
	if err != nil {
		return nil, err
	}
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

// applyUpdates is the shared mutation engine: it walks the schema's fields and
// applies edits to targetObj, then parses the result to ensure validity. The
// baseIndent argument is the leading whitespace for newly-inserted lines and
// determines the file's nesting context (map entry vs singleton root).
func applyUpdates(src []byte, targetObj *hclsyntax.ObjectConsExpr, values map[string]any, resource *schema.Resource, baseIndent string) ([]byte, error) {
	edits := newEditBuilder(src, baseIndent)
	for _, step := range resource.Steps {
		for _, field := range step.Fields {
			val, ok := values[field.Path]
			if !ok {
				continue
			}
			path := strings.Split(field.Path, ".")
			edits.update(targetObj, path, val, field)
		}
	}
	out := edits.apply()
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("modified HCL is invalid: %w", err)
	}
	return out, nil
}

// editBuilder accumulates byte-range replacements and applies them in
// descending start order so each subsequent edit's offsets stay valid.
type editBuilder struct {
	src        []byte
	baseIndent string
	nestIndent string
	edits      []textEdit
}

func newEditBuilder(src []byte, baseIndent string) *editBuilder {
	return &editBuilder{
		src:        src,
		baseIndent: baseIndent,
		nestIndent: baseIndent + "  ",
	}
}

func (b *editBuilder) update(obj *hclsyntax.ObjectConsExpr, path []string, val any, field schema.Field) {
	targetItem := findObjectItem(obj, path[0])

	if len(path) == 1 {
		newText := formatValue(val, field.Type)
		if targetItem != nil {
			b.edits = append(b.edits, textEdit{
				start: targetItem.ValueExpr.Range().Start.Byte,
				end:   targetItem.ValueExpr.Range().End.Byte,
				text:  newText,
			})
			return
		}
		insertPos := addFieldInsertPoint(b.src, obj)
		indent := b.indentFor(field.Path)
		b.edits = append(b.edits, textEdit{
			start: insertPos,
			end:   insertPos,
			text:  fmt.Sprintf("%s%s = %s\n", indent, path[0], newText),
		})
		return
	}

	if targetItem != nil {
		nestedObj, ok := targetItem.ValueExpr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			b.edits = append(b.edits, textEdit{
				start: targetItem.ValueExpr.Range().Start.Byte,
				end:   targetItem.ValueExpr.Range().End.Byte,
				text:  b.renderInlineNestedBlock(path[1], val, field.Type),
			})
			return
		}
		b.update(nestedObj, path[1:], val, field)
		return
	}

	insertPos := addFieldInsertPoint(b.src, obj)
	b.edits = append(b.edits, textEdit{
		start: insertPos,
		end:   insertPos,
		text:  b.renderNewNestedBlock(path, val, field.Type),
	})
}

// indentFor returns the indent prefix for a flat or nested field path.
func (b *editBuilder) indentFor(fieldPath string) string {
	if strings.Contains(fieldPath, ".") {
		return b.nestIndent
	}
	return b.baseIndent
}

// renderInlineNestedBlock generates a `{ key = val }` literal for replacing a
// non-object value with a new nested block in place.
func (b *editBuilder) renderInlineNestedBlock(key string, val any, fType string) string {
	return fmt.Sprintf("{\n%s%s = %s\n%s}", b.nestIndent, key, formatValue(val, fType), b.baseIndent)
}

// renderNewNestedBlock generates a brand-new nested block when the parent key
// did not exist in the source. Paths deeper than two segments produce nested
// braces matching the schema's path nesting.
func (b *editBuilder) renderNewNestedBlock(path []string, val any, fType string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s%s = {\n", b.baseIndent, path[0])
	currPath := path[1:]
	indent := b.nestIndent
	for len(currPath) > 0 {
		if len(currPath) == 1 {
			fmt.Fprintf(&sb, "%s%s = %s\n", indent, currPath[0], formatValue(val, fType))
			break
		}
		fmt.Fprintf(&sb, "%s%s = {\n", indent, currPath[0])
		currPath = currPath[1:]
		indent += "  "
	}
	for len(indent) > len(b.nestIndent) {
		indent = indent[:len(indent)-2]
		fmt.Fprintf(&sb, "%s}\n", indent)
	}
	fmt.Fprintf(&sb, "%s}\n", b.baseIndent)
	return sb.String()
}

func (b *editBuilder) apply() []byte {
	sort.Slice(b.edits, func(i, j int) bool {
		return b.edits[i].start > b.edits[j].start
	})
	out := b.src
	for _, e := range b.edits {
		var res bytes.Buffer
		res.Write(out[:e.start])
		res.WriteString(e.text)
		res.Write(out[e.end:])
		out = res.Bytes()
	}
	return out
}

// findChildObject returns the object value of a given key under rootObj, or an
// error referencing varName for clearer diagnostics.
func findChildObject(rootObj *hclsyntax.ObjectConsExpr, key, varName string) (*hclsyntax.ObjectConsExpr, error) {
	for _, item := range rootObj.Items {
		name, err := exprToString(item.KeyExpr)
		if err != nil || name != key {
			continue
		}
		obj, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return nil, fmt.Errorf("value for key %q is not an object", key)
		}
		return obj, nil
	}
	return nil, fmt.Errorf("key %q not found in %q", key, varName)
}

// findObjectItem returns the matching item from obj.Items, or nil when absent.
func findObjectItem(obj *hclsyntax.ObjectConsExpr, key string) *hclsyntax.ObjectConsItem {
	for i := range obj.Items {
		k, err := exprToString(obj.Items[i].KeyExpr)
		if err == nil && k == key {
			return &obj.Items[i]
		}
	}
	return nil
}

func findRootMap(src []byte, path string) (*hclsyntax.ObjectConsExpr, error) {
	localsBody, err := localsBlockBody(src)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty map path")
	}
	attr, ok := localsBody.Attributes[parts[0]]
	if !ok {
		return nil, fmt.Errorf("attribute %q not found in locals block", parts[0])
	}
	currObj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, fmt.Errorf("attribute %q is not an object expression", parts[0])
	}
	for _, key := range parts[1:] {
		next, err := descend(currObj, key, path)
		if err != nil {
			return nil, err
		}
		currObj = next
	}
	return currObj, nil
}

func descend(obj *hclsyntax.ObjectConsExpr, key, path string) (*hclsyntax.ObjectConsExpr, error) {
	for _, item := range obj.Items {
		k, err := exprToString(item.KeyExpr)
		if err != nil || k != key {
			continue
		}
		next, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return nil, fmt.Errorf("part %q in path is not an object expression", key)
		}
		return next, nil
	}
	return nil, fmt.Errorf("key %q in path %q not found", key, path)
}

func findObjectClosingBrace(src []byte, obj *hclsyntax.ObjectConsExpr) (int, error) {
	end := obj.SrcRange.End.Byte
	pos := end - 1
	for pos > 0 && src[pos] != '}' {
		pos--
	}
	lineStart := pos
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	return lineStart, nil
}

func findResourceRange(src []byte, varName, key string) (int, int, error) {
	rootObj, err := findRootMap(src, varName)
	if err != nil {
		return 0, 0, err
	}
	for _, item := range rootObj.Items {
		name, err := exprToString(item.KeyExpr)
		if err != nil || name != key {
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
	return 0, 0, fmt.Errorf("key %q not found in %q", key, varName)
}

// formatValue renders a schema-typed Go value as an HCL literal.
func formatValue(val any, fType string) string {
	switch fType {
	case schema.TypeString, schema.TypeSelect:
		return fmt.Sprintf("%q", val)
	case schema.TypeBoolean:
		return fmt.Sprintf("%t", val)
	case schema.TypeInteger, schema.TypeNumber:
		return fmt.Sprintf("%v", val)
	case schema.TypeListString:
		return formatStringList(val)
	case schema.TypeListInteger:
		return formatIntList(val)
	case schema.TypeMapString:
		return formatStringMap(val)
	}
	return "null"
}

func formatStringList(val any) string {
	switch v := val.(type) {
	case []string:
		parts := make([]string, 0, len(v))
		for _, p := range v {
			parts = append(parts, fmt.Sprintf("%q", p))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		var parts []string
		for _, p := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				parts = append(parts, fmt.Sprintf("%q", trimmed))
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return "[]"
}

func formatIntList(val any) string {
	switch v := val.(type) {
	case []int:
		parts := make([]string, 0, len(v))
		for _, p := range v {
			parts = append(parts, fmt.Sprintf("%v", p))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		var parts []string
		for _, p := range strings.Split(v, ",") {
			trimmed := strings.TrimSpace(p)
			if trimmed == "" {
				continue
			}
			if _, err := strconv.Atoi(trimmed); err == nil {
				parts = append(parts, trimmed)
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return "[]"
}

func formatStringMap(val any) string {
	m, ok := val.(map[string]string)
	if !ok {
		return "{}"
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%q = %q", k, v))
	}
	sort.Strings(parts)
	return "{ " + strings.Join(parts, ", ") + " }"
}

// renderResourceEntry renders a new map_entry block ready to splice into the
// existing locals map. The tree intermediate ensures schemas with multiple
// fields under the same nested object collapse to one set of braces.
func renderResourceEntry(key string, values map[string]any, resource *schema.Resource) string {
	type node struct {
		isVal bool
		val   string
		child map[string]*node
	}
	root := &node{child: make(map[string]*node)}

	for _, step := range resource.Steps {
		for _, field := range step.Fields {
			val, ok := values[field.Path]
			if !ok || val == nil {
				continue
			}
			parts := strings.Split(field.Path, ".")
			curr := root
			for i, part := range parts {
				if i == len(parts)-1 {
					curr.child[part] = &node{isVal: true, val: formatValue(val, field.Type)}
					continue
				}
				if _, exists := curr.child[part]; !exists {
					curr.child[part] = &node{child: make(map[string]*node)}
				}
				curr = curr.child[part]
			}
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "    %q = {\n", key)

	var render func(n *node, indent string)
	render = func(n *node, indent string) {
		keys := make([]string, 0, len(n.child))
		for k := range n.child {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		maxLen := 0
		for _, k := range keys {
			if len(k) > maxLen {
				maxLen = len(k)
			}
		}

		for _, k := range keys {
			child := n.child[k]
			pad := strings.Repeat(" ", maxLen-len(k))
			if child.isVal {
				fmt.Fprintf(&sb, "%s%s%s = %s\n", indent, k, pad, child.val)
				continue
			}
			fmt.Fprintf(&sb, "%s%s = {\n", indent, k)
			render(child, indent+"  ")
			fmt.Fprintf(&sb, "%s}\n", indent)
		}
	}
	render(root, "      ")
	sb.WriteString("    }\n")
	return sb.String()
}

// ExistingResourceKeys returns the keys of the map at varName, used to populate
// target selectors for update/delete flows.
func ExistingResourceKeys(src []byte, varName string) ([]string, error) {
	rootObj, err := findRootMap(src, varName)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rootObj.Items))
	for _, item := range rootObj.Items {
		name, err := exprToString(item.KeyExpr)
		if err != nil {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}
