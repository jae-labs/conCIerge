package hcl

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

type textEdit struct {
	start int
	end   int
	text  string
}

func Parse(src []byte) (*hcl.File, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, "locals.tf")
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	return file, nil
}

func localsBlockBody(src []byte) (*hclsyntax.Body, error) {
	file, err := Parse(src)
	if err != nil {
		return nil, err
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("root body is not hclsyntax body")
	}
	for _, block := range body.Blocks {
		if block.Type == "locals" {
			return block.Body, nil
		}
	}
	return nil, fmt.Errorf("locals block not found")
}

func exprToString(expr hclsyntax.Expression) (string, error) {
	switch v := expr.(type) {
	case *hclsyntax.ObjectConsKeyExpr:
		return exprToString(v.Wrapped)
	case *hclsyntax.LiteralValueExpr:
		return v.Val.AsString(), nil
	case *hclsyntax.TemplateExpr:
		if len(v.Parts) == 1 {
			return exprToString(v.Parts[0])
		}
	case *hclsyntax.ScopeTraversalExpr:
		if len(v.Traversal) == 0 {
			return "", fmt.Errorf("empty traversal")
		}
		name := v.Traversal.RootName()
		for _, traverser := range v.Traversal[1:] {
			attr, ok := traverser.(hcl.TraverseAttr)
			if !ok {
				return "", fmt.Errorf("unsupported traversal segment")
			}
			name += "." + attr.Name
		}
		return name, nil
	}
	return "", fmt.Errorf("unsupported string expression")
}

func exprToBool(expr hclsyntax.Expression) (bool, error) {
	lit, ok := expr.(*hclsyntax.LiteralValueExpr)
	if !ok {
		return false, fmt.Errorf("expected boolean literal")
	}
	return lit.Val.True(), nil
}

func exprToInt(expr hclsyntax.Expression) (int, error) {
	lit, ok := expr.(*hclsyntax.LiteralValueExpr)
	if !ok {
		return 0, fmt.Errorf("expected integer literal")
	}
	if !lit.Val.Type().Equals(cty.Number) {
		return 0, fmt.Errorf("expected numeric literal")
	}
	i, _ := lit.Val.AsBigFloat().Int64()
	return int(i), nil
}

func exprToFloat(expr hclsyntax.Expression) (float64, error) {
	lit, ok := expr.(*hclsyntax.LiteralValueExpr)
	if !ok {
		return 0, fmt.Errorf("expected numeric literal")
	}
	if !lit.Val.Type().Equals(cty.Number) {
		return 0, fmt.Errorf("expected numeric literal")
	}
	f, _ := lit.Val.AsBigFloat().Float64()
	return f, nil
}

func addFieldInsertPoint(src []byte, obj *hclsyntax.ObjectConsExpr) int {
	end := obj.SrcRange.End.Byte
	pos := end - 1
	for pos > 0 && src[pos] != '}' {
		pos--
	}
	lineStart := pos
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	return lineStart
}
