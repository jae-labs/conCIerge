package config

import (
	"testing"
)

func setAllEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SLACK_MODE", "")
	t.Setenv("SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_SIGNING_SECRET", "signing-secret")
	t.Setenv("SLACK_HTTP_LISTEN_ADDR", "127.0.0.1:9000")
	t.Setenv("SLACK_REQUESTS_CHANNEL_ID", "C12345")
	t.Setenv("SLACK_USER_IDS", "U111,U222")
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_INSTALLATION_ID", "67890")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----")
	t.Setenv("GITHUB_OWNER", "test-org")
	t.Setenv("GITHUB_REPO", "test-repo")
}

func TestLoad_missingRequired(t *testing.T) {
	required := []string{
		"SLACK_APP_TOKEN", "SLACK_BOT_TOKEN", "SLACK_REQUESTS_CHANNEL_ID",
		"GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY",
		"GITHUB_OWNER", "GITHUB_REPO",
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
	if cfg.SlackMode != SlackModeSocket {
		t.Errorf("got SlackMode=%q, want %q", cfg.SlackMode, SlackModeSocket)
	}
	if cfg.SlackHTTPListenAddr != "127.0.0.1:9000" {
		t.Errorf("got SlackHTTPListenAddr=%q, want 127.0.0.1:9000", cfg.SlackHTTPListenAddr)
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
	if cfg.SlackRequestsChannelID != "C12345" {
		t.Errorf("got SlackRequestsChannelID=%q, want C12345", cfg.SlackRequestsChannelID)
	}
}

func TestLoad_normalizesEscapedGitHubPrivateKey(t *testing.T) {
	setAllEnv(t)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", `-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----"
	if string(cfg.GitHubAppPrivateKey) != want {
		t.Fatalf("got GitHubAppPrivateKey=%q, want %q", string(cfg.GitHubAppPrivateKey), want)
	}
}

func TestLoad_httpModeRequiresSigningSecretButNotAppToken(t *testing.T) {
	setAllEnv(t)
	t.Setenv("SLACK_MODE", SlackModeHTTP)
	t.Setenv("SLACK_APP_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SlackMode != SlackModeHTTP {
		t.Fatalf("got SlackMode=%q, want %q", cfg.SlackMode, SlackModeHTTP)
	}
}

func TestLoad_httpModeMissingSigningSecret(t *testing.T) {
	setAllEnv(t)
	t.Setenv("SLACK_MODE", SlackModeHTTP)
	t.Setenv("SLACK_SIGNING_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when SLACK_SIGNING_SECRET is missing in http mode")
	}
}

func TestLoad_invalidSlackMode(t *testing.T) {
	setAllEnv(t)
	t.Setenv("SLACK_MODE", "weird")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid SLACK_MODE")
	}
}

func TestLoad_httpModeDefaultListenAddr(t *testing.T) {
	setAllEnv(t)
	t.Setenv("SLACK_MODE", SlackModeHTTP)
	t.Setenv("SLACK_HTTP_LISTEN_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SlackHTTPListenAddr != DefaultHTTPListenAddr {
		t.Fatalf("got SlackHTTPListenAddr=%q, want %q", cfg.SlackHTTPListenAddr, DefaultHTTPListenAddr)
	}
}

func TestLoad_observabilityDefaults(t *testing.T) {
	setAllEnv(t)
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_ENVIRONMENT", "")
	t.Setenv("SERVICE_VERSION", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "")
	t.Setenv("METRICS_ENABLED", "")
	t.Setenv("METRICS_LISTEN_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OtelServiceName != "concierge" {
		t.Errorf("got OtelServiceName=%q, want \"concierge\"", cfg.OtelServiceName)
	}
	if cfg.OtelEnvironment != "development" {
		t.Errorf("got OtelEnvironment=%q, want \"development\"", cfg.OtelEnvironment)
	}
	if cfg.ServiceVersion != "dev" {
		t.Errorf("got ServiceVersion=%q, want \"dev\"", cfg.ServiceVersion)
	}
	if cfg.OtelTracesEndpoint != DefaultOTLPGRPCEndpoint {
		t.Errorf("got OtelTracesEndpoint=%q, want %q", cfg.OtelTracesEndpoint, DefaultOTLPGRPCEndpoint)
	}
	if cfg.OtelTracesProtocol != "grpc" {
		t.Errorf("got OtelTracesProtocol=%q, want \"grpc\"", cfg.OtelTracesProtocol)
	}
	if cfg.OtelMetricsEndpoint != DefaultOTLPGRPCEndpoint {
		t.Errorf("got OtelMetricsEndpoint=%q, want %q", cfg.OtelMetricsEndpoint, DefaultOTLPGRPCEndpoint)
	}
	if cfg.OtelMetricsProtocol != "grpc" {
		t.Errorf("got OtelMetricsProtocol=%q, want \"grpc\"", cfg.OtelMetricsProtocol)
	}
	if cfg.MetricsEnabled {
		t.Error("expected MetricsEnabled=false by default")
	}
	if cfg.MetricsListenAddr != DefaultMetricsListenAddr {
		t.Errorf("got MetricsListenAddr=%q, want %q", cfg.MetricsListenAddr, DefaultMetricsListenAddr)
	}
}

func TestLoad_observabilityFromEnv(t *testing.T) {
	setAllEnv(t)
	t.Setenv("OTEL_SERVICE_NAME", "my-service")
	t.Setenv("OTEL_ENVIRONMENT", "production")
	t.Setenv("SERVICE_VERSION", "2026.06.03")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "localhost:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "grpc")
	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("METRICS_LISTEN_ADDR", "127.0.0.1:9100")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OtelServiceName != "my-service" {
		t.Errorf("got OtelServiceName=%q, want \"my-service\"", cfg.OtelServiceName)
	}
	if cfg.OtelEnvironment != "production" {
		t.Errorf("got OtelEnvironment=%q, want \"production\"", cfg.OtelEnvironment)
	}
	if cfg.ServiceVersion != "2026.06.03" {
		t.Errorf("got ServiceVersion=%q, want \"2026.06.03\"", cfg.ServiceVersion)
	}
	if cfg.OtelTracesEndpoint != "localhost:4317" {
		t.Errorf("got OtelTracesEndpoint=%q, want \"localhost:4317\"", cfg.OtelTracesEndpoint)
	}
	if cfg.OtelTracesProtocol != "http/protobuf" {
		t.Errorf("got OtelTracesProtocol=%q, want \"http/protobuf\"", cfg.OtelTracesProtocol)
	}
	if cfg.OtelMetricsEndpoint != "localhost:4318" {
		t.Errorf("got OtelMetricsEndpoint=%q, want \"localhost:4318\"", cfg.OtelMetricsEndpoint)
	}
	if cfg.OtelMetricsProtocol != "grpc" {
		t.Errorf("got OtelMetricsProtocol=%q, want \"grpc\"", cfg.OtelMetricsProtocol)
	}
	if !cfg.MetricsEnabled {
		t.Error("expected MetricsEnabled=true")
	}
	if cfg.MetricsListenAddr != "127.0.0.1:9100" {
		t.Errorf("got MetricsListenAddr=%q, want \"127.0.0.1:9100\"", cfg.MetricsListenAddr)
	}
}

func TestLoad_invalidMetricsEnabled(t *testing.T) {
	setAllEnv(t)
	t.Setenv("METRICS_ENABLED", "definitely")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid METRICS_ENABLED value")
	}
}

func TestLoad_metricsListenAddrMustBeLoopback(t *testing.T) {
	setAllEnv(t)
	t.Setenv("METRICS_LISTEN_ADDR", "1.1.1.1:9100")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-loopback metrics listener")
	}
}

func TestLoad_invalidOTLPProtocol(t *testing.T) {
	setAllEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "udp")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid OTEL_EXPORTER_OTLP_PROTOCOL")
	}
}

func TestLoad_invalidOTLPMetricsProtocol(t *testing.T) {
	setAllEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "udp")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid OTEL_EXPORTER_OTLP_METRICS_PROTOCOL")
	}
}

func TestLoad_commitAuthorOverrides(t *testing.T) {
	setAllEnv(t)
	t.Setenv("GITHUB_COMMIT_AUTHOR_NAME", "Ops Bot")
	t.Setenv("GITHUB_COMMIT_AUTHOR_EMAIL", "ops-bot@example.invalid")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GitHubCommitAuthorName != "Ops Bot" {
		t.Errorf("got name=%q", cfg.GitHubCommitAuthorName)
	}
	if cfg.GitHubCommitAuthorEmail != "ops-bot@example.invalid" {
		t.Errorf("got email=%q", cfg.GitHubCommitAuthorEmail)
	}
}

func TestLoad_commitAuthorEmptyByDefault(t *testing.T) {
	setAllEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GitHubCommitAuthorName != "" || cfg.GitHubCommitAuthorEmail != "" {
		t.Errorf("expected empty defaults so github.NewClient applies its own; got %q/%q",
			cfg.GitHubCommitAuthorName, cfg.GitHubCommitAuthorEmail)
	}
}
