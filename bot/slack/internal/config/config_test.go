package config

import (
	"testing"
)

func setAllEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_REQUESTS_CHANNEL_ID", "C12345")
	t.Setenv("SLACK_USER_IDS", "U111,U222")
	t.Setenv("SLACK_MANAGER_IDS", "U111")
	t.Setenv("SLACK_ADMIN_IDS", "U111")
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_INSTALLATION_ID", "67890")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----")
	t.Setenv("GITHUB_OWNER", "test-org")
	t.Setenv("GITHUB_REPO", "test-repo")
	t.Setenv("LINEAR_API_KEY", "lin_api_test")
	t.Setenv("LINEAR_TEAM_ID", "team-uuid")
}

func TestLoad_missingRequired(t *testing.T) {
	required := []string{
		"SLACK_APP_TOKEN", "SLACK_BOT_TOKEN", "SLACK_REQUESTS_CHANNEL_ID",
		"GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY",
		"GITHUB_OWNER", "GITHUB_REPO",
		"LINEAR_API_KEY", "LINEAR_TEAM_ID",
	}
	for _, key := range required {
		t.Run(key, func(t *testing.T) {
			setAllEnv(t)
			t.Setenv(key, "")
			_, err := Load()
			if err == nil {
				t.Fatalf("expected error when %s is missing", key)
			}
		})
	}
}

func TestLoad_invalidAppID(t *testing.T) {
	setAllEnv(t)
	t.Setenv("GITHUB_APP_ID", "not-a-number")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-integer GITHUB_APP_ID")
	}
}

func TestLoad_invalidInstallationID(t *testing.T) {
	setAllEnv(t)
	t.Setenv("GITHUB_APP_INSTALLATION_ID", "not-a-number")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-integer GITHUB_APP_INSTALLATION_ID")
	}
}

func TestLoad_valid(t *testing.T) {
	setAllEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SlackAppToken != "xapp-test" {
		t.Errorf("got SlackAppToken=%q, want xapp-test", cfg.SlackAppToken)
	}
	if cfg.GitHubAppID != 12345 {
		t.Errorf("got GitHubAppID=%d, want 12345", cfg.GitHubAppID)
	}
	if cfg.GitHubAppInstallationID != 67890 {
		t.Errorf("got GitHubAppInstallationID=%d, want 67890", cfg.GitHubAppInstallationID)
	}
	if cfg.GitHubOwner != "test-org" {
		t.Errorf("got GitHubOwner=%q, want test-org", cfg.GitHubOwner)
	}
}
