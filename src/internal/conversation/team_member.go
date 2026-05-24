package conversation

// TeamMemberConfig holds parameters collected during the team member wizard.
type TeamMemberConfig struct {
	Team     string // team name
	Username string // GitHub username from members map
	Role     string // "member" or "maintainer"
}
