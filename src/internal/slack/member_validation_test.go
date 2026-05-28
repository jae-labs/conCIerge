package slack

import "testing"

func TestValidateTeamMemberAdd(t *testing.T) {
	tests := []struct {
		name     string
		team     string
		username string
		role     string
		wantErr  bool
	}{
		{"valid", "Maintainers", "alice", "member", false},
		{"valid maintainer", "Maintainers", "alice", "maintainer", false},
		{"empty team", "", "alice", "member", true},
		{"empty username", "Maintainers", "", "member", true},
		{"empty role", "Maintainers", "alice", "", true},
		{"invalid role", "Maintainers", "alice", "admin", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTeamMemberAdd(tt.team, tt.username, tt.role)
			if (err != "") != tt.wantErr {
				t.Errorf("validateTeamMemberAdd() = %q, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTeamMemberRemove(t *testing.T) {
	tests := []struct {
		name     string
		team     string
		username string
		wantErr  bool
	}{
		{"valid", "Maintainers", "alice", false},
		{"empty team", "", "alice", true},
		{"empty username", "Maintainers", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTeamMemberRemove(tt.team, tt.username)
			if (err != "") != tt.wantErr {
				t.Errorf("validateTeamMemberRemove() = %q, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTeamMemberChangeRole(t *testing.T) {
	tests := []struct {
		name    string
		team    string
		user    string
		role    string
		wantErr bool
	}{
		{"valid", "Maintainers", "alice", "maintainer", false},
		{"invalid role", "Maintainers", "alice", "owner", true},
		{"empty fields", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTeamMemberChangeRole(tt.team, tt.user, tt.role)
			if (err != "") != tt.wantErr {
				t.Errorf("validateTeamMemberChangeRole() = %q, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
