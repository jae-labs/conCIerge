package hcl

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/jae-labs/concierge/internal/schema"
)

func testRepoResource() *schema.Resource {
	return &schema.Resource{
		ID:       "repo",
		RootPath: "repos",
		Steps: []schema.Step{
			{
				ID:    "basics",
				Title: "Basic Information",
				Fields: []schema.Field{
					{Path: "description", Type: schema.TypeString, Label: "Description", Required: true},
					{Path: "visibility", Type: schema.TypeSelect, Label: "Visibility", Options: []string{"public", "private"}},
					{Path: "has_issues", Type: schema.TypeBoolean, Label: "Has Issues"},
					{Path: "topics", Type: schema.TypeListString, Label: "Topics"},
					{Path: "team_access", Type: schema.TypeMapString, Label: "Team Access"},
					{Path: "branch_protection.required_reviews", Type: schema.TypeInteger, Label: "Required Reviews"},
					{Path: "branch_protection.require_linear_history", Type: schema.TypeBoolean, Label: "Linear History"},
				},
			},
		},
	}
}

func testMembershipResource() *schema.Resource {
	return &schema.Resource{
		ID:   "user_management",
		Kind: schema.KindMembership,
		File: "github/locals.tf",
		Steps: []schema.Step{{ID: "membership", Fields: []schema.Field{
			{Path: "team", Type: schema.TypeSelect, Label: "Team", Required: true},
			{Path: "username", Type: schema.TypeSelect, Label: "Username", Required: true},
			{Path: "role", Type: schema.TypeSelect, Label: "Role", Required: true},
		}}},
	}
}

func loadMembershipFixture(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile("testdata/locals_members.tf")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return src
}

func TestReadResource(t *testing.T) {
	src := []byte(`locals {
  repos = {
    "terraform" = {
      description = "Terraform IaC"
      visibility  = "public"
      has_issues  = true
      topics      = ["terraform", "iac"]
      team_access = { "Maintainers" = "admin" }
      branch_protection = {
        required_reviews       = 1
        require_linear_history = true
      }
    }
  }
}
`)

	values, err := ReadResource(src, "repos", "terraform", testRepoResource())
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}

	if values["description"] != "Terraform IaC" {
		t.Errorf("expected description 'Terraform IaC', got %v", values["description"])
	}
	if values["visibility"] != "public" {
		t.Errorf("expected visibility 'public', got %v", values["visibility"])
	}
	if values["has_issues"] != true {
		t.Errorf("expected has_issues true, got %v", values["has_issues"])
	}

	topics, ok := values["topics"].([]string)
	if !ok || len(topics) != 2 || topics[0] != "terraform" || topics[1] != "iac" {
		t.Errorf("expected topics list, got %v", values["topics"])
	}

	teamAccess, ok := values["team_access"].(map[string]string)
	if !ok || teamAccess["Maintainers"] != "admin" {
		t.Errorf("expected team_access mapping Maintainers->admin, got %v", values["team_access"])
	}

	if values["branch_protection.required_reviews"] != 1 {
		t.Errorf("expected branch_protection.required_reviews 1, got %v", values["branch_protection.required_reviews"])
	}
	if values["branch_protection.require_linear_history"] != true {
		t.Errorf("expected branch_protection.require_linear_history true, got %v", values["branch_protection.require_linear_history"])
	}
}

func TestAddResource(t *testing.T) {
	src := []byte(`locals {
  repos = {
    "terraform" = {
      description = "Terraform IaC"
      visibility  = "public"
      has_issues  = true
    }
  }
}
`)

	newVals := map[string]any{
		"description": "New Project",
		"visibility":  "private",
		"has_issues":  false,
		"topics":      []string{"new", "project"},
	}

	out, err := AddResource(src, "repos", "new-project", newVals, testRepoResource())
	if err != nil {
		t.Fatalf("AddResource failed: %v", err)
	}

	values, err := ReadResource(out, "repos", "new-project", testRepoResource())
	if err != nil {
		t.Fatalf("ReadResource failed on new-project: %v", err)
	}

	if values["description"] != "New Project" {
		t.Errorf("expected 'New Project', got %v", values["description"])
	}
	if values["visibility"] != "private" {
		t.Errorf("expected 'private', got %v", values["visibility"])
	}
	if values["has_issues"] != false {
		t.Errorf("expected false, got %v", values["has_issues"])
	}
}

func TestUpdateResource(t *testing.T) {
	src := []byte(`locals {
  repos = {
    "terraform" = {
      description = "Terraform IaC"
      visibility  = "public"
      has_issues  = true
      team_access = { "Maintainers" = "admin" }
      branch_protection = {
        required_reviews = 1
      }
    }
  }
}
`)

	updates := map[string]any{
		"description":                        "Updated Terraform IaC",
		"visibility":                         "private",
		"branch_protection.required_reviews": 2,
	}

	out, err := UpdateResource(src, "repos", "terraform", updates, testRepoResource())
	if err != nil {
		t.Fatalf("UpdateResource failed: %v", err)
	}

	values, err := ReadResource(out, "repos", "terraform", testRepoResource())
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}

	if values["description"] != "Updated Terraform IaC" {
		t.Errorf("expected description 'Updated Terraform IaC', got %v", values["description"])
	}
	if values["visibility"] != "private" {
		t.Errorf("expected visibility 'private', got %v", values["visibility"])
	}
	if values["branch_protection.required_reviews"] != 2 {
		t.Errorf("expected branch_protection.required_reviews 2, got %v", values["branch_protection.required_reviews"])
	}
	if values["has_issues"] != true {
		t.Errorf("expected has_issues true, got %v", values["has_issues"])
	}
}

func TestRemoveResource(t *testing.T) {
	src := []byte(`locals {
  repos = {
    "terraform" = {
      description = "Terraform IaC"
    }
    "catv" = {
      description = "Flashcards"
    }
  }
}
`)

	out, err := RemoveResource(src, "repos", "terraform")
	if err != nil {
		t.Fatalf("RemoveResource failed: %v", err)
	}

	if strings.Contains(string(out), "terraform") {
		t.Errorf("expected terraform to be removed, got: %s", string(out))
	}
	if !strings.Contains(string(out), "catv") {
		t.Errorf("expected catv to be preserved, got: %s", string(out))
	}
}

func TestUpdateSingleton(t *testing.T) {
	src := []byte(`locals {
  org_settings = {
    name                          = "JAE Labs"
    location                      = "Ireland"
    web_commit_signoff_required   = false
    default_repository_permission = "none"
  }
}
`)

	resource := &schema.Resource{
		ID:       "org_settings",
		Kind:     schema.KindSingleton,
		RootPath: "org_settings",
		Steps: []schema.Step{{
			ID:    "settings",
			Title: "Settings",
			Fields: []schema.Field{
				{Path: "name", Type: schema.TypeString, Label: "Name"},
				{Path: "location", Type: schema.TypeString, Label: "Location"},
				{Path: "web_commit_signoff_required", Type: schema.TypeBoolean, Label: "Signoff"},
			},
		}},
	}

	values, err := ReadSingleton(src, "org_settings", resource)
	if err != nil {
		t.Fatalf("ReadSingleton failed: %v", err)
	}
	if values["name"] != "JAE Labs" {
		t.Fatalf("unexpected singleton read values: %v", values)
	}

	out, err := UpdateSingleton(src, "org_settings", map[string]any{
		"name":                        "JAE Labs Updated",
		"web_commit_signoff_required": true,
	}, resource)
	if err != nil {
		t.Fatalf("UpdateSingleton failed: %v", err)
	}

	updatedValues, err := ReadSingleton(out, "org_settings", resource)
	if err != nil {
		t.Fatalf("ReadSingleton after update failed: %v", err)
	}
	if updatedValues["name"] != "JAE Labs Updated" {
		t.Fatalf("unexpected updated name: %v", updatedValues["name"])
	}
	if updatedValues["web_commit_signoff_required"] != true {
		t.Fatalf("unexpected updated bool: %v", updatedValues["web_commit_signoff_required"])
	}
}

func TestApplyMembershipActionAdd(t *testing.T) {
	src := loadMembershipFixture(t)

	out, err := ApplyMembershipAction(src, map[string]any{
		"team":     "Maintainers",
		"username": "jane-doe",
		"role":     "member",
	}, schema.ActionAdd, testMembershipResource())
	if err != nil {
		t.Fatalf("ApplyMembershipAction add failed: %v", err)
	}

	values, err := ReadMembership(out, "Maintainers", "jane-doe")
	if err != nil {
		t.Fatalf("ReadMembership failed: %v", err)
	}
	if values["role"] != "member" {
		t.Fatalf("unexpected role: %v", values["role"])
	}
	if !bytes.Contains(out, []byte("all_repo_admin")) {
		t.Fatal("org_roles corrupted after membership add")
	}
}

func TestApplyMembershipActionDeleteStrict(t *testing.T) {
	src := loadMembershipFixture(t)

	if _, err := ApplyMembershipAction(src, map[string]any{
		"team":     "Collaborators",
		"username": "nobody",
	}, schema.ActionDelete, testMembershipResource()); err == nil {
		t.Fatal("expected strict delete error for missing member")
	}
}

func TestApplyMembershipActionChangeRole(t *testing.T) {
	src := loadMembershipFixture(t)

	out, err := ApplyMembershipAction(src, map[string]any{
		"team":     "Collaborators",
		"username": "jane-doe",
		"role":     "maintainer",
	}, schema.ActionChangeRole, testMembershipResource())
	if err != nil {
		t.Fatalf("ApplyMembershipAction change_role failed: %v", err)
	}

	values, err := ReadMembership(out, "Collaborators", "jane-doe")
	if err != nil {
		t.Fatalf("ReadMembership failed: %v", err)
	}
	if values["role"] != "maintainer" {
		t.Fatalf("unexpected role: %v", values["role"])
	}
}
