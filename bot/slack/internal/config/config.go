package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	SlackAppToken           string
	SlackBotToken           string
	SlackRequestsChannelID  string
	SlackUserIDs            map[string]bool
	SlackManagerIDs         map[string]bool
	SlackAdminIDs           map[string]bool
	GitHubAppID             int64
	GitHubAppInstallationID int64
	GitHubAppPrivateKey     []byte
	GitHubOwner             string
	GitHubRepo              string
	LinearAPIKey            string
	LinearTeamID            string
}

// parseIDList splits a comma-separated list of Slack user IDs into a set.
func parseIDList(env string) map[string]bool {
	raw := os.Getenv(env)
	if raw == "" {
		return map[string]bool{}
	}
	ids := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			ids[id] = true
		}
	}
	return ids
}

func Load() (*Config, error) {
	// best-effort overload; .env values take precedence over shell env vars
	_ = godotenv.Overload()

	required := []string{
		"SLACK_APP_TOKEN", "SLACK_BOT_TOKEN", "SLACK_REQUESTS_CHANNEL_ID",
		"GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY",
		"GITHUB_OWNER", "GITHUB_REPO",
		"LINEAR_API_KEY", "LINEAR_TEAM_ID",
	}
	for _, key := range required {
		if os.Getenv(key) == "" {
			return nil, fmt.Errorf("missing required env var: %s", key)
		}
	}

	appID, err := strconv.ParseInt(os.Getenv("GITHUB_APP_ID"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("GITHUB_APP_ID must be an integer: %w", err)
	}

	installationID, err := strconv.ParseInt(os.Getenv("GITHUB_APP_INSTALLATION_ID"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("GITHUB_APP_INSTALLATION_ID must be an integer: %w", err)
	}

	cfg := &Config{
		SlackAppToken:           os.Getenv("SLACK_APP_TOKEN"),
		SlackBotToken:           os.Getenv("SLACK_BOT_TOKEN"),
		SlackRequestsChannelID:  os.Getenv("SLACK_REQUESTS_CHANNEL_ID"),
		SlackUserIDs:            parseIDList("SLACK_USER_IDS"),
		SlackManagerIDs:         parseIDList("SLACK_MANAGER_IDS"),
		SlackAdminIDs:           parseIDList("SLACK_ADMIN_IDS"),
		GitHubAppID:             appID,
		GitHubAppInstallationID: installationID,
		GitHubAppPrivateKey:     []byte(os.Getenv("GITHUB_APP_PRIVATE_KEY")),
		GitHubOwner:             os.Getenv("GITHUB_OWNER"),
		GitHubRepo:              os.Getenv("GITHUB_REPO"),
		LinearAPIKey:            os.Getenv("LINEAR_API_KEY"),
		LinearTeamID:            os.Getenv("LINEAR_TEAM_ID"),
	}

	return cfg, nil
}
