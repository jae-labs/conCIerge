package schema

import (
	"fmt"
	"slices"
	"sort"

	yaml "gopkg.in/yaml.v3"
)

const (
	ActionAdd        = "add"
	ActionDelete     = "delete"
	ActionSettings   = "settings"
	ActionChangeRole = "change_role"

	TypeString      = "string"
	TypeInteger     = "integer"
	TypeNumber      = "number"
	TypeBoolean     = "boolean"
	TypeSelect      = "select"
	TypeListString  = "list_string"
	TypeListInteger = "list_integer"
	TypeMapString   = "map_string"

	KindMapEntry   = "map_entry"
	KindSingleton  = "singleton"
	KindMembership = "membership"
)

var validActions = []string{ActionAdd, ActionDelete, ActionSettings, ActionChangeRole}
var validTypes = []string{TypeString, TypeInteger, TypeNumber, TypeBoolean, TypeSelect, TypeListString, TypeListInteger, TypeMapString}
var validKinds = []string{KindMapEntry, KindSingleton, KindMembership}

type Schema struct {
	Version    int        `yaml:"version"`
	Categories []Category `yaml:"categories"`
	Resources  []Resource `yaml:"resources"`
}

type Category struct {
	ID    string `yaml:"id"`
	Label string `yaml:"label"`
	Order int    `yaml:"order"`
}

type Resource struct {
	ID         string   `yaml:"id"`
	Category   string   `yaml:"category"`
	Kind       string   `yaml:"kind"`
	Label      string   `yaml:"label"`
	File       string   `yaml:"file"`
	RootPath   string   `yaml:"root_path"`
	KeyLabel   string   `yaml:"key_label"`
	KeyPattern string   `yaml:"key_pattern"`
	Actions    []string `yaml:"actions"`
	Steps      []Step   `yaml:"steps"`
}

type Step struct {
	ID     string  `yaml:"id"`
	Title  string  `yaml:"title"`
	Fields []Field `yaml:"fields"`
}

type Field struct {
	Path         string     `yaml:"path"`
	Type         string     `yaml:"type"`
	Label        string     `yaml:"label"`
	Required     bool       `yaml:"required"`
	Default      any        `yaml:"default"`
	Options      []string   `yaml:"options"`
	KeySource    *KeySource `yaml:"key_source"`
	ValueOptions []string   `yaml:"value_options"`
}

type KeySource struct {
	File     string `yaml:"file"`
	RootPath string `yaml:"root_path"`
}

func Parse(src []byte) (*Schema, error) {
	var s Schema
	if err := yaml.Unmarshal(src, &s); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	for i := range s.Resources {
		if s.Resources[i].Kind == "" {
			s.Resources[i].Kind = KindMapEntry
		}
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	sort.Slice(s.Categories, func(i, j int) bool {
		if s.Categories[i].Order == s.Categories[j].Order {
			return s.Categories[i].ID < s.Categories[j].ID
		}
		return s.Categories[i].Order < s.Categories[j].Order
	})
	return &s, nil
}

func (s *Schema) Validate() error {
	if s.Version == 0 {
		return fmt.Errorf("schema version is required")
	}

	categoryIDs := make(map[string]struct{}, len(s.Categories))
	categoryOrders := make(map[int]struct{}, len(s.Categories))
	for _, category := range s.Categories {
		if category.ID == "" {
			return fmt.Errorf("category id is required")
		}
		if category.Label == "" {
			return fmt.Errorf("category %q label is required", category.ID)
		}
		if _, exists := categoryIDs[category.ID]; exists {
			return fmt.Errorf("duplicate category id %q", category.ID)
		}
		if _, exists := categoryOrders[category.Order]; exists {
			return fmt.Errorf("duplicate category order %d", category.Order)
		}
		categoryIDs[category.ID] = struct{}{}
		categoryOrders[category.Order] = struct{}{}
	}

	resourceIDs := make(map[string]struct{}, len(s.Resources))
	for _, resource := range s.Resources {
		if resource.ID == "" {
			return fmt.Errorf("resource id is required")
		}
		if _, exists := resourceIDs[resource.ID]; exists {
			return fmt.Errorf("duplicate resource id %q", resource.ID)
		}
		resourceIDs[resource.ID] = struct{}{}

		if _, exists := categoryIDs[resource.Category]; !exists {
			return fmt.Errorf("resource %q references unknown category %q", resource.ID, resource.Category)
		}
		if resource.Kind == "" {
			resource.Kind = KindMapEntry
		}
		if !slices.Contains(validKinds, resource.Kind) {
			return fmt.Errorf("resource %q has unsupported kind %q", resource.ID, resource.Kind)
		}
		if resource.File == "" {
			return fmt.Errorf("resource %q file is required", resource.ID)
		}
		if resource.RootPath == "" && resource.Kind != KindMembership {
			return fmt.Errorf("resource %q root_path is required", resource.ID)
		}
		if resource.KeyLabel == "" {
			if resource.Kind == KindMapEntry {
				return fmt.Errorf("resource %q key_label is required", resource.ID)
			}
		}
		if len(resource.Actions) == 0 {
			return fmt.Errorf("resource %q actions are required", resource.ID)
		}
		if len(resource.Steps) == 0 {
			return fmt.Errorf("resource %q steps are required", resource.ID)
		}

		fieldPaths := make(map[string]struct{})
		for _, action := range resource.Actions {
			if !slices.Contains(validActions, action) {
				return fmt.Errorf("resource %q has unsupported action %q", resource.ID, action)
			}
		}
		for _, step := range resource.Steps {
			if step.ID == "" {
				return fmt.Errorf("resource %q step id is required", resource.ID)
			}
			if step.Title == "" {
				return fmt.Errorf("resource %q step %q title is required", resource.ID, step.ID)
			}
			for _, field := range step.Fields {
				if field.Path == "" {
					return fmt.Errorf("resource %q step %q field path is required", resource.ID, step.ID)
				}
				if _, exists := fieldPaths[field.Path]; exists {
					return fmt.Errorf("resource %q has duplicate field path %q", resource.ID, field.Path)
				}
				fieldPaths[field.Path] = struct{}{}
				if field.Label == "" {
					return fmt.Errorf("resource %q field %q label is required", resource.ID, field.Path)
				}
				if !slices.Contains(validTypes, field.Type) {
					return fmt.Errorf("resource %q field %q has unsupported type %q", resource.ID, field.Path, field.Type)
				}
				if field.KeySource != nil {
					if field.KeySource.File == "" || field.KeySource.RootPath == "" {
						return fmt.Errorf("resource %q field %q key_source file and root_path are required", resource.ID, field.Path)
					}
				}
				if field.Type == TypeSelect && len(field.Options) == 0 && field.KeySource == nil {
					return fmt.Errorf("resource %q field %q select options or key_source are required", resource.ID, field.Path)
				}
				if field.Type == TypeMapString && field.KeySource == nil {
					return fmt.Errorf("resource %q field %q key_source is required", resource.ID, field.Path)
				}
			}
		}
	}

	return nil
}

func (s *Schema) ResourcesByCategory(categoryID string) []Resource {
	var resources []Resource
	for _, resource := range s.Resources {
		if resource.Category == categoryID {
			resources = append(resources, resource)
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Label < resources[j].Label
	})
	return resources
}

func (s *Schema) ResourceByID(resourceID string) (*Resource, bool) {
	for i := range s.Resources {
		if s.Resources[i].ID == resourceID {
			return &s.Resources[i], true
		}
	}
	return nil, false
}
