package schema

import "testing"

func TestParseSchema(t *testing.T) {
	input := []byte(`
version: 1
categories:
  - id: github
    label: GitHub
    order: 10
resources:
  - id: repo
    category: github
    label: GitHub Repositories
    file: github/locals.tf
    root_path: repos
    key_label: Repository Name
    key_pattern: "^[A-Za-z0-9._-]+$"
    actions: [add, settings, delete]
    steps:
      - id: basics
        title: Basics
        fields:
          - path: description
            type: string
            label: Description
            required: true
`)

	s, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if s.Version != 1 {
		t.Fatalf("Version=%d", s.Version)
	}
	if len(s.Categories) != 1 {
		t.Fatalf("Categories=%d", len(s.Categories))
	}
	if len(s.Resources) != 1 {
		t.Fatalf("Resources=%d", len(s.Resources))
	}
	if got := s.Resources[0].RootPath; got != "repos" {
		t.Fatalf("RootPath=%q", got)
	}
	if got := s.Resources[0].Kind; got != KindMapEntry {
		t.Fatalf("Kind=%q", got)
	}
	if got := s.Resources[0].Steps[0].Fields[0].Path; got != "description" {
		t.Fatalf("Field Path=%q", got)
	}
}

func TestParseSchemaSingletonAndMembershipKinds(t *testing.T) {
	input := []byte(`
version: 1
categories:
  - id: github
    label: GitHub
    order: 10
resources:
  - id: org_settings
    kind: singleton
    category: github
    label: Organization Settings
    file: github/locals.tf
    root_path: org_settings
    actions: [settings]
    steps:
      - id: basics
        title: Basics
        fields:
          - path: name
            type: string
            label: Name
            required: true
  - id: user_management
    kind: membership
    category: github
    label: User Management
    file: github/locals.tf
    actions: [add, delete, change_role]
    steps:
      - id: basics
        title: Basics
        fields:
          - path: team
            type: select
            label: Team
            required: true
            options: [Maintainers]
`)

	s, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if s.Resources[0].Kind != KindSingleton {
		t.Fatalf("org_settings kind=%q", s.Resources[0].Kind)
	}
	if s.Resources[1].Kind != KindMembership {
		t.Fatalf("user_management kind=%q", s.Resources[1].Kind)
	}
}

func TestValidateRejectsUnknownCategory(t *testing.T) {
	input := []byte(`
version: 1
categories:
  - id: github
    label: GitHub
    order: 10
resources:
  - id: repo
    category: missing
    label: GitHub Repositories
    file: github/locals.tf
    root_path: repos
    key_label: Repository Name
    actions: [add]
    steps:
      - id: basics
        title: Basics
        fields:
          - path: description
            type: string
            label: Description
            required: true
`)

	_, err := Parse(input)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsMissingSelectOptions(t *testing.T) {
	input := []byte(`
version: 1
categories:
  - id: github
    label: GitHub
    order: 10
resources:
  - id: repo
    category: github
    label: GitHub Repositories
    file: github/locals.tf
    root_path: repos
    key_label: Repository Name
    actions: [add]
    steps:
      - id: basics
        title: Basics
        fields:
          - path: visibility
            type: select
            label: Visibility
            required: true
`)

	_, err := Parse(input)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateAllowsDynamicSelectOptionsViaKeySource(t *testing.T) {
	input := []byte(`
version: 1
categories:
  - id: github
    label: GitHub
    order: 10
resources:
  - id: user_management
    kind: membership
    category: github
    label: User Management
    file: github/locals.tf
    actions: [add]
    steps:
      - id: basics
        title: Basics
        fields:
          - path: team
            type: select
            label: Team
            required: true
            key_source:
              file: github/locals.tf
              root_path: teams
`)

	_, err := Parse(input)
	if err != nil {
		t.Fatalf("expected dynamic select to validate, got: %v", err)
	}
}

func TestValidateAllowsNumberAndListIntegerTypes(t *testing.T) {
	input := []byte(`
version: 1
categories:
  - id: devops
    label: DevOps
    order: 10
resources:
  - id: custom_monitoring
    category: devops
    file: monitoring/locals.tf
    root_path: monitor
    key_label: ID
    actions: [add]
    steps:
      - id: metrics
        title: Metrics configuration
        fields:
          - path: objective
            type: number
            label: Objective percentage
            required: true
          - path: alert_destinations
            type: list_integer
            label: Target channel IDs
            required: false
`)

	s, err := Parse(input)
	if err != nil {
		t.Fatalf("expected number and list_integer fields to validate, got: %v", err)
	}
	if s.Resources[0].Steps[0].Fields[0].Type != TypeNumber {
		t.Fatalf("expected TypeNumber, got: %s", s.Resources[0].Steps[0].Fields[0].Type)
	}
	if s.Resources[0].Steps[0].Fields[1].Type != TypeListInteger {
		t.Fatalf("expected TypeListInteger, got: %s", s.Resources[0].Steps[0].Fields[1].Type)
	}
}
