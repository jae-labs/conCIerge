package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const (
	SlackModeSocket          = "socket"
	SlackModeHTTP            = "http"
	DefaultHTTPListenAddr    = "127.0.0.1:8080"
	DefaultMetricsListenAddr = "127.0.0.1:9090"
)

type Config struct {
	SlackMode               string
	SlackAppToken           string
	SlackBotToken           string
	SlackSigningSecret      string
	SlackHTTPListenAddr     string
	SlackRequestsChannelID  string
	SlackUserIDs            map[string]bool
	SlackManagerIDs         map[string]bool
	SlackAdminIDs           map[string]bool
	GitHubAppID             int64
	GitHubAppInstallationID int64
	GitHubAppPrivateKey     []byte
	GitHubOwner             string
	GitHubRepo              string

	// Observability
	OtelServiceName    string // OTEL_SERVICE_NAME; default "concierge"
	OtelEnvironment    string // OTEL_ENVIRONMENT; default "development"
	OtelTracesEndpoint string // OTEL_EXPORTER_OTLP_ENDPOINT; empty = no OTLP export
	OtelTracesProtocol string // OTEL_EXPORTER_OTLP_PROTOCOL; default "grpc"
	MetricsEnabled     bool   // METRICS_ENABLED; default false
	MetricsListenAddr  string // METRICS_LISTEN_ADDR; default "127.0.0.1:9090"
	SentryDSN          string // SENTRY_DSN; optional
	SentryEnvironment  string // SENTRY_ENVIRONMENT; defaults to OTEL_ENVIRONMENT
	SentryRelease      string // SENTRY_RELEASE; optional
}

func normalizeEnvMultiline(value string) string {
	return strings.ReplaceAll(value, `\n`, "\n")
}

// getEnv retrieves environment variables, falling back to Doppler CONCIERGE_GH_ prefixed variables for GitHub configuration if standard ones are empty.
func getEnv(key string) string {
	val := os.Getenv(key)
	if val != "" {
		return val
	}
	switch key {
	case "GITHUB_APP_ID":
		return os.Getenv("CONCIERGE_GH_APP_ID")
	case "GITHUB_APP_INSTALLATION_ID":
		return os.Getenv("CONCIERGE_GH_APP_INSTALLATION_ID")
	case "GITHUB_APP_PRIVATE_KEY":
		return os.Getenv("CONCIERGE_GH_APP_PRIVATE_KEY")
	case "GITHUB_OWNER":
		return os.Getenv("CONCIERGE_GH_OWNER")
	case "GITHUB_REPO":
		return os.Getenv("CONCIERGE_GH_REPO")
	}
	return ""
}

// parseIDList splits a comma-separated list of Slack user IDs into a set.
func parseIDList(env string) map[string]bool {
	raw := getEnv(env)
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

	mode := strings.TrimSpace(strings.ToLower(getEnv("SLACK_MODE")))
	if mode == "" {
		mode = SlackModeSocket
	}
	if mode != SlackModeSocket && mode != SlackModeHTTP {
		return nil, fmt.Errorf("SLACK_MODE must be %q or %q", SlackModeSocket, SlackModeHTTP)
	}

	required := []string{
		"SLACK_BOT_TOKEN", "SLACK_REQUESTS_CHANNEL_ID",
		"GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY",
		"GITHUB_OWNER", "GITHUB_REPO",
	}
	if mode == SlackModeSocket {
		required = append(required, "SLACK_APP_TOKEN")
	}
	if mode == SlackModeHTTP {
		required = append(required, "SLACK_SIGNING_SECRET")
	}
	for _, key := range required {
		if getEnv(key) == "" {
			return nil, fmt.Errorf("missing required env var: %s", key)
		}
	}

	appID, err := strconv.ParseInt(getEnv("GITHUB_APP_ID"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("GITHUB_APP_ID must be an integer: %w", err)
	}

	installationID, err := strconv.ParseInt(getEnv("GITHUB_APP_INSTALLATION_ID"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("GITHUB_APP_INSTALLATION_ID must be an integer: %w", err)
	}

	cfg := &Config{
		SlackMode:               mode,
		SlackAppToken:           getEnv("SLACK_APP_TOKEN"),
		SlackBotToken:           getEnv("SLACK_BOT_TOKEN"),
		SlackSigningSecret:      getEnv("SLACK_SIGNING_SECRET"),
		SlackHTTPListenAddr:     getEnv("SLACK_HTTP_LISTEN_ADDR"),
		SlackRequestsChannelID:  getEnv("SLACK_REQUESTS_CHANNEL_ID"),
		SlackUserIDs:            parseIDList("SLACK_USER_IDS"),
		SlackManagerIDs:         parseIDList("SLACK_MANAGER_IDS"),
		SlackAdminIDs:           parseIDList("SLACK_ADMIN_IDS"),
		GitHubAppID:             appID,
		GitHubAppInstallationID: installationID,
		GitHubAppPrivateKey:     []byte(normalizeEnvMultiline(strings.Trim(getEnv("GITHUB_APP_PRIVATE_KEY"), "\""))),
		GitHubOwner:             getEnv("GITHUB_OWNER"),
		GitHubRepo:              getEnv("GITHUB_REPO"),
	}
	if cfg.SlackHTTPListenAddr == "" {
		cfg.SlackHTTPListenAddr = DefaultHTTPListenAddr
	}

	cfg.OtelServiceName = getEnv("OTEL_SERVICE_NAME")
	if cfg.OtelServiceName == "" {
		cfg.OtelServiceName = "concierge"
	}
	cfg.OtelEnvironment = getEnv("OTEL_ENVIRONMENT")
	if cfg.OtelEnvironment == "" {
		cfg.OtelEnvironment = "development"
	}
	cfg.OtelTracesEndpoint = getEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	cfg.OtelTracesProtocol = strings.ToLower(strings.TrimSpace(getEnv("OTEL_EXPORTER_OTLP_PROTOCOL")))
	if cfg.OtelTracesProtocol == "" {
		cfg.OtelTracesProtocol = "grpc"
	}
	if cfg.OtelTracesProtocol != "grpc" && cfg.OtelTracesProtocol != "http" && cfg.OtelTracesProtocol != "http/protobuf" && cfg.OtelTracesProtocol != "http/json" {
		return nil, fmt.Errorf("OTEL_EXPORTER_OTLP_PROTOCOL must be one of grpc, http, http/protobuf, http/json")
	}
	metricsEnabled := strings.TrimSpace(getEnv("METRICS_ENABLED"))
	if metricsEnabled != "" {
		parsed, err := strconv.ParseBool(metricsEnabled)
		if err != nil {
			return nil, fmt.Errorf("METRICS_ENABLED must be a boolean: %w", err)
		}
		cfg.MetricsEnabled = parsed
	}
	cfg.MetricsListenAddr = getEnv("METRICS_LISTEN_ADDR")
	if cfg.MetricsListenAddr == "" {
		cfg.MetricsListenAddr = DefaultMetricsListenAddr
	}
	if err := validateLoopbackListenAddr("METRICS_LISTEN_ADDR", cfg.MetricsListenAddr); err != nil {
		return nil, err
	}

	cfg.SentryDSN = getEnv("SENTRY_DSN")
	cfg.SentryEnvironment = getEnv("SENTRY_ENVIRONMENT")
	if cfg.SentryEnvironment == "" {
		cfg.SentryEnvironment = cfg.OtelEnvironment
	}
	cfg.SentryRelease = getEnv("SENTRY_RELEASE")

	return cfg, nil
}

func validateLoopbackListenAddr(name, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s must be a host:port pair: %w", name, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s must use a loopback host", name)
	}
	return nil
}
