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
	DefaultOTLPGRPCEndpoint  = "127.0.0.1:4317"
)

type Config struct {
	SlackMode               string
	SlackAppToken           string
	SlackBotToken           string
	SlackSigningSecret      string
	SlackHTTPListenAddr     string
	SlackRequestsChannelID  string
	SlackUserIDs            map[string]bool
	GitHubAppID             int64
	GitHubAppInstallationID int64
	GitHubAppPrivateKey     []byte
	GitHubOwner             string
	GitHubRepo              string
	GitHubCommitAuthorName  string // GITHUB_COMMIT_AUTHOR_NAME; default "conCierge Bot"
	GitHubCommitAuthorEmail string // GITHUB_COMMIT_AUTHOR_EMAIL; default "239121271+luiz1361@users.noreply.github.com"

	// Observability
	OtelServiceName     string // OTEL_SERVICE_NAME; default "concierge"
	OtelEnvironment     string // OTEL_ENVIRONMENT; default "development"
	ServiceVersion      string // SERVICE_VERSION; default "dev"
	OtelTracesEndpoint  string // OTEL_EXPORTER_OTLP_ENDPOINT; default "127.0.0.1:4317"
	OtelTracesProtocol  string // OTEL_EXPORTER_OTLP_PROTOCOL; default "grpc"
	OtelMetricsEndpoint string // OTEL_EXPORTER_OTLP_METRICS_ENDPOINT; default OTEL_EXPORTER_OTLP_ENDPOINT
	OtelMetricsProtocol string // OTEL_EXPORTER_OTLP_METRICS_PROTOCOL; fallback OTEL_EXPORTER_OTLP_PROTOCOL
	MetricsEnabled      bool   // METRICS_ENABLED; default false
	MetricsListenAddr   string // METRICS_LISTEN_ADDR; default "127.0.0.1:9090"
}

func normalizeEnvMultiline(value string) string {
	return strings.ReplaceAll(value, `\n`, "\n")
}

// getEnv retrieves environment variables, falling back to Doppler CONCIERGE_ prefixed variables if standard ones are empty.
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
	case "SLACK_BOT_TOKEN":
		return os.Getenv("CONCIERGE_SLACK_BOT_TOKEN")
	case "SLACK_SIGNING_SECRET":
		return os.Getenv("CONCIERGE_SLACK_SIGNING_SECRET")
	case "SLACK_REQUESTS_CHANNEL_ID":
		return os.Getenv("CONCIERGE_SLACK_REQUESTS_CHANNEL_ID")
	case "SLACK_USER_IDS":
		return os.Getenv("CONCIERGE_SLACK_USER_IDS")
	case "SLACK_APP_TOKEN":
		return os.Getenv("CONCIERGE_SLACK_APP_TOKEN")
	case "SLACK_MODE":
		return os.Getenv("CONCIERGE_SLACK_MODE")
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
		GitHubAppID:             appID,
		GitHubAppInstallationID: installationID,
		GitHubAppPrivateKey:     []byte(normalizeEnvMultiline(strings.Trim(getEnv("GITHUB_APP_PRIVATE_KEY"), "\""))),
		GitHubOwner:             getEnv("GITHUB_OWNER"),
		GitHubRepo:              getEnv("GITHUB_REPO"),
		GitHubCommitAuthorName:  getEnv("GITHUB_COMMIT_AUTHOR_NAME"),
		GitHubCommitAuthorEmail: getEnv("GITHUB_COMMIT_AUTHOR_EMAIL"),
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
	cfg.ServiceVersion = strings.TrimSpace(getEnv("SERVICE_VERSION"))
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "dev"
	}
	cfg.OtelTracesEndpoint = getEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if cfg.OtelTracesEndpoint == "" {
		cfg.OtelTracesEndpoint = DefaultOTLPGRPCEndpoint
	}
	cfg.OtelTracesProtocol = strings.ToLower(strings.TrimSpace(getEnv("OTEL_EXPORTER_OTLP_PROTOCOL")))
	if cfg.OtelTracesProtocol == "" {
		cfg.OtelTracesProtocol = "grpc"
	}
	if err := validateOTLPProtocol("OTEL_EXPORTER_OTLP_PROTOCOL", cfg.OtelTracesProtocol); err != nil {
		return nil, err
	}
	cfg.OtelMetricsEndpoint = getEnv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")
	if cfg.OtelMetricsEndpoint == "" {
		cfg.OtelMetricsEndpoint = cfg.OtelTracesEndpoint
	}
	cfg.OtelMetricsProtocol = strings.ToLower(strings.TrimSpace(getEnv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL")))
	if cfg.OtelMetricsProtocol == "" {
		cfg.OtelMetricsProtocol = cfg.OtelTracesProtocol
	}
	if err := validateOTLPProtocol("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", cfg.OtelMetricsProtocol); err != nil {
		return nil, err
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

	return cfg, nil
}

func validateOTLPProtocol(name, protocol string) error {
	switch protocol {
	case "grpc", "http", "http/protobuf", "http/json":
		return nil
	default:
		return fmt.Errorf("%s must be one of grpc, http, http/protobuf, http/json", name)
	}
}

func validateLoopbackListenAddr(name, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s must be a host:port pair: %w", name, err)
	}
	if host == "localhost" || host == "0.0.0.0" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s must use a loopback host", name)
	}
	return nil
}
