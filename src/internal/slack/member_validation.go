package slack

// validateTeamMemberAdd checks form completeness for adding a member to a team.
// Returns an error message string, or empty string if valid.
func validateTeamMemberAdd(team, username, role string) string {
	if team == "" {
		return "Team is required."
	}
	if username == "" {
		return "Member is required."
	}
	if role == "" {
		return "Role is required."
	}
	if role != "member" && role != "maintainer" {
		return "Role must be member or maintainer."
	}
	return ""
}

// validateTeamMemberRemove checks form completeness for removing a member from a team.
func validateTeamMemberRemove(team, username string) string {
	if team == "" {
		return "Team is required."
	}
	if username == "" {
		return "Member is required."
	}
	return ""
}

// validateTeamMemberChangeRole checks form completeness for changing a member's role.
func validateTeamMemberChangeRole(team, username, role string) string {
	if team == "" {
		return "Team is required."
	}
	if username == "" {
		return "Member is required."
	}
	if role == "" {
		return "Role is required."
	}
	if role != "member" && role != "maintainer" {
		return "Role must be member or maintainer."
	}
	return ""
}
