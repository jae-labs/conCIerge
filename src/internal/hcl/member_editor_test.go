package hcl

import (
	"bytes"
	"os"
	"testing"
)

func loadMembersFixture(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile("testdata/locals_members.tf")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return src
}

func TestExtractMemberNames(t *testing.T) {
	src := loadMembersFixture(t)
	names, err := ExtractMemberNames(src)
	if err != nil {
		t.Fatalf("ExtractMemberNames: %v", err)
	}
	want := []string{"abubakar-abiona", "jane-doe", "luiz1361"}
	if len(names) != len(want) {
		t.Fatalf("got %d names, want %d", len(names), len(want))
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestExtractTeamMembers(t *testing.T) {
	src := loadMembersFixture(t)

	members, maintainers, err := ExtractTeamMembers(src, "Collaborators")
	if err != nil {
		t.Fatalf("ExtractTeamMembers: %v", err)
	}
	if len(members) != 2 || members[0] != "abubakar-abiona" || members[1] != "jane-doe" {
		t.Errorf("members = %v, want [abubakar-abiona, jane-doe]", members)
	}
	if len(maintainers) != 1 || maintainers[0] != "luiz1361" {
		t.Errorf("maintainers = %v, want [luiz1361]", maintainers)
	}
}

func TestExtractTeamMembers_NotFound(t *testing.T) {
	src := loadMembersFixture(t)
	_, _, err := ExtractTeamMembers(src, "NonExistent")
	if err == nil {
		t.Fatal("expected error for non-existent team")
	}
}

func TestIsTeamMember(t *testing.T) {
	src := loadMembersFixture(t)

	tests := []struct {
		team, user string
		wantIn     bool
		wantRole   string
	}{
		{"Collaborators", "abubakar-abiona", true, "member"},
		{"Collaborators", "luiz1361", true, "maintainer"},
		{"Collaborators", "jane-doe", true, "member"},
		{"Collaborators", "nobody", false, ""},
		{"Maintainers", "luiz1361", true, "maintainer"},
	}
	for _, tt := range tests {
		in, role, err := IsTeamMember(src, tt.team, tt.user)
		if err != nil {
			t.Fatalf("IsTeamMember(%s, %s): %v", tt.team, tt.user, err)
		}
		if in != tt.wantIn || role != tt.wantRole {
			t.Errorf("IsTeamMember(%s, %s) = (%v, %q), want (%v, %q)",
				tt.team, tt.user, in, role, tt.wantIn, tt.wantRole)
		}
	}
}

func TestAddTeamMember_NewMember(t *testing.T) {
	src := loadMembersFixture(t)
	result, err := AddTeamMember(src, "Maintainers", "jane-doe", "member")
	if err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	in, role, _ := IsTeamMember(result, "Maintainers", "jane-doe")
	if !in || role != "member" {
		t.Errorf("jane-doe not added as member: in=%v role=%s", in, role)
	}
}

func TestAddTeamMember_OrgRolesPreserved(t *testing.T) {
	src := loadMembersFixture(t)
	result, err := AddTeamMember(src, "Maintainers", "jane-doe", "member")
	if err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	if _, err := Parse(result); err != nil {
		t.Fatalf("result HCL is invalid: %v", err)
	}
	if !bytes.Contains(result, []byte("all_repo_admin")) {
		t.Error("org_roles corrupted after AddTeamMember")
	}
}

func TestAddTeamMember_MoveRole(t *testing.T) {
	src := loadMembersFixture(t)
	result, err := AddTeamMember(src, "Collaborators", "abubakar-abiona", "maintainer")
	if err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	in, role, _ := IsTeamMember(result, "Collaborators", "abubakar-abiona")
	if !in || role != "maintainer" {
		t.Errorf("expected maintainer, got in=%v role=%s", in, role)
	}
}

func TestAddTeamMember_SameRole(t *testing.T) {
	src := loadMembersFixture(t)
	_, err := AddTeamMember(src, "Collaborators", "abubakar-abiona", "member")
	if err == nil {
		t.Fatal("expected error for same role")
	}
}

func TestAddTeamMember_EmptyList(t *testing.T) {
	fixture := []byte(`locals {
  members = {
    "alice" = { role = "member", full_name = "Alice" }
  }

  teams = {
    "Empty" = {
      description = "Empty team"
      privacy     = "closed"
      members     = []
      maintainers = []
    }
  }
}
`)
	result, err := AddTeamMember(fixture, "Empty", "alice", "member")
	if err != nil {
		t.Fatalf("AddTeamMember to empty list: %v", err)
	}
	in, role, _ := IsTeamMember(result, "Empty", "alice")
	if !in || role != "member" {
		t.Errorf("alice not added: in=%v role=%s", in, role)
	}
}

func TestRemoveTeamMember_FromMembers(t *testing.T) {
	src := loadMembersFixture(t)
	result, err := RemoveTeamMember(src, "Collaborators", "jane-doe")
	if err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	in, _, _ := IsTeamMember(result, "Collaborators", "jane-doe")
	if in {
		t.Error("jane-doe still in team after removal")
	}
	if _, err := Parse(result); err != nil {
		t.Fatalf("invalid output HCL: %v", err)
	}
}

func TestRemoveTeamMember_FromMaintainers(t *testing.T) {
	src := loadMembersFixture(t)
	result, err := RemoveTeamMember(src, "Collaborators", "luiz1361")
	if err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	in, _, _ := IsTeamMember(result, "Collaborators", "luiz1361")
	if in {
		t.Error("luiz1361 still in team after removal")
	}
}

func TestRemoveTeamMember_NotInTeam(t *testing.T) {
	src := loadMembersFixture(t)
	_, err := RemoveTeamMember(src, "Collaborators", "nobody")
	if err == nil {
		t.Fatal("expected error for non-member")
	}
}

func TestRemoveTeamMember_LastMember(t *testing.T) {
	src := loadMembersFixture(t)
	result, err := RemoveTeamMember(src, "Maintainers", "luiz1361")
	if err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	_, maintainers, _ := ExtractTeamMembers(result, "Maintainers")
	if len(maintainers) != 0 {
		t.Errorf("expected empty maintainers, got %v", maintainers)
	}
	if _, err := Parse(result); err != nil {
		t.Fatalf("invalid output HCL: %v", err)
	}
}

func TestUpdateTeamMemberRole_MemberToMaintainer(t *testing.T) {
	src := loadMembersFixture(t)
	result, err := UpdateTeamMemberRole(src, "Collaborators", "abubakar-abiona", "maintainer")
	if err != nil {
		t.Fatalf("UpdateTeamMemberRole: %v", err)
	}
	in, role, _ := IsTeamMember(result, "Collaborators", "abubakar-abiona")
	if !in || role != "maintainer" {
		t.Errorf("expected maintainer, got in=%v role=%s", in, role)
	}
}

func TestUpdateTeamMemberRole_MaintainerToMember(t *testing.T) {
	src := loadMembersFixture(t)
	result, err := UpdateTeamMemberRole(src, "Collaborators", "luiz1361", "member")
	if err != nil {
		t.Fatalf("UpdateTeamMemberRole: %v", err)
	}
	in, role, _ := IsTeamMember(result, "Collaborators", "luiz1361")
	if !in || role != "member" {
		t.Errorf("expected member, got in=%v role=%s", in, role)
	}
}

func TestUpdateTeamMemberRole_NotInTeam(t *testing.T) {
	src := loadMembersFixture(t)
	_, err := UpdateTeamMemberRole(src, "Collaborators", "nobody", "member")
	if err == nil {
		t.Fatal("expected error for non-member")
	}
}

func TestUpdateTeamMemberRole_SameRole(t *testing.T) {
	src := loadMembersFixture(t)
	_, err := UpdateTeamMemberRole(src, "Collaborators", "abubakar-abiona", "member")
	if err == nil {
		t.Fatal("expected error for same role")
	}
}
