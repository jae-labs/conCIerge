package hcl

import (
	"os"
	"reflect"
	"testing"

	"github.com/jae-labs/conCIerge/internal/conversation"
)

func loadDopplerTestdata(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile("testdata/locals_projects.tf")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return src
}

func TestDopplerExistingProjects(t *testing.T) {
	src := loadDopplerTestdata(t)
	names, err := ExistingProjectNames(src)
	if err != nil {
		t.Fatalf("ExistingProjectNames: %v", err)
	}

	want := []string{"concierge", "github"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("ExistingProjectNames = %v, want %v", names, want)
	}
}

func TestDopplerExtractProjectConfig(t *testing.T) {
	src := loadDopplerTestdata(t)
	cfg, err := ExtractProjectConfig(src, "concierge")
	if err != nil {
		t.Fatalf("ExtractProjectConfig: %v", err)
	}

	wantDesc := "A Slack Bot written in GoLang that provisions resources, manages access, and automates workflows across various platforms via Terraform."
	if cfg.Description != wantDesc {
		t.Errorf("Description = %q, want %q", cfg.Description, wantDesc)
	}
}

func TestDopplerAddProject(t *testing.T) {
	src := loadDopplerTestdata(t)
	newCfg := conversation.DopplerProjectConfig{
		Name:        "new-project",
		Description: "This is a new project",
	}

	out, err := AddProject(src, newCfg)
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	names, err := ExistingProjectNames(out)
	if err != nil {
		t.Fatalf("ExistingProjectNames after add: %v", err)
	}

	want := []string{"concierge", "github", "new-project"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names after add = %v, want %v", names, want)
	}

	cfg, err := ExtractProjectConfig(out, "new-project")
	if err != nil {
		t.Fatalf("ExtractProjectConfig of new project: %v", err)
	}
	if cfg.Description != "This is a new project" {
		t.Errorf("Description of new project = %q, want %q", cfg.Description, "This is a new project")
	}
}

func TestDopplerRemoveProject(t *testing.T) {
	src := loadDopplerTestdata(t)

	out, err := RemoveProject(src, "github")
	if err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}

	names, err := ExistingProjectNames(out)
	if err != nil {
		t.Fatalf("ExistingProjectNames after remove: %v", err)
	}

	want := []string{"concierge"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names after remove = %v, want %v", names, want)
	}
}

func TestDopplerUpdateProject(t *testing.T) {
	src := loadDopplerTestdata(t)
	newCfg := conversation.DopplerProjectConfig{
		Name:        "concierge",
		Description: "Updated bot description",
	}

	out, err := UpdateProject(src, "concierge", newCfg)
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	cfg, err := ExtractProjectConfig(out, "concierge")
	if err != nil {
		t.Fatalf("ExtractProjectConfig after update: %v", err)
	}

	if cfg.Description != "Updated bot description" {
		t.Errorf("Description after update = %q, want %q", cfg.Description, "Updated bot description")
	}
}
