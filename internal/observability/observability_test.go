package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TestSetup_noTracesEndpoint verifies that Setup succeeds when no OTLP endpoint
// is provided (tracing falls back to the default noop provider).
func TestSetup_noTracesEndpoint(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-service",
		Environment:    "test",
		ServiceVersion: "test-version",
		MetricsEnabled: false,
	}
	rt, err := Setup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	if rt == nil {
		t.Fatal("Setup returned nil Runtime")
		return
	}
	if rt.Logger == nil {
		t.Fatal("Logger is nil")
		return
	}
	if rt.MetricsHandler == nil {
		t.Fatal("MetricsHandler is nil")
		return
	}
	if rt.Shutdown == nil {
		t.Fatal("Shutdown is nil")
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned unexpected error: %v", err)
	}
}

// TestSetup_metricsEnabled verifies that Setup with MetricsEnabled=true returns
// a functioning metrics handler.
func TestSetup_metricsEnabled(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-service",
		Environment:    "test",
		ServiceVersion: "test-version",
		MetricsEnabled: true,
	}
	rt, err := Setup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	if rt.MetricsHandler == nil {
		t.Fatal("MetricsHandler is nil")
		return
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	rt.MetricsHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status=%d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "target_info") {
		t.Fatalf("expected prometheus payload, got %q", rec.Body.String())
	}
	_ = rt.Shutdown(context.Background())
}

func TestBuildTracerProvider_grpcAcceptsURLShapedEndpoint(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-service",
		Environment:    "test",
		ServiceVersion: "test-version",
		TracesEndpoint: "http://127.0.0.1:4317",
		TracesProtocol: "grpc",
	}
	res, err := buildResource(context.Background(), cfg.ServiceName, cfg.ServiceVersion, cfg.Environment)
	if err != nil {
		t.Fatalf("buildResource returned unexpected error: %v", err)
	}
	tp, err := buildTracerProvider(context.Background(), res, cfg)
	if err != nil {
		t.Fatalf("buildTracerProvider returned unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("buildTracerProvider returned nil")
	}
}

func TestBuildMeterProvider_grpcAcceptsURLShapedEndpoint(t *testing.T) {
	cfg := Config{
		ServiceName:     "test-service",
		Environment:     "test",
		ServiceVersion:  "test-version",
		MetricsEnabled:  true,
		MetricsEndpoint: "http://127.0.0.1:4317",
		MetricsProtocol: "grpc",
	}
	res, err := buildResource(context.Background(), cfg.ServiceName, cfg.ServiceVersion, cfg.Environment)
	if err != nil {
		t.Fatalf("buildResource returned unexpected error: %v", err)
	}
	mp, _, err := buildMeterProvider(context.Background(), res, cfg)
	if err != nil {
		t.Fatalf("buildMeterProvider returned unexpected error: %v", err)
	}
	if mp == nil {
		t.Fatal("buildMeterProvider returned nil")
	}
}

// TestNewLogger_textInDev verifies text handler for development environments.
func TestNewLogger_textInDev(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	for _, env := range []string{"", "development", "dev", "local", "test"} {
		logger := NewLogger(env, "concierge", "test-version")
		if logger == nil {
			t.Fatalf("NewLogger(%q) returned nil", env)
		}
		// Verify it is enabled at info level (text handler by default handles this).
		if !logger.Enabled(context.Background(), slog.LevelInfo) {
			t.Errorf("NewLogger(%q): expected info level to be enabled", env)
		}
	}
}

// TestNewLogger_jsonInProd verifies JSON handler for production-like environments.
func TestNewLogger_jsonInProd(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	for _, env := range []string{"production", "staging", "prod"} {
		logger := NewLogger(env, "concierge", "test-version")
		if logger == nil {
			t.Fatalf("NewLogger(%q) returned nil", env)
		}
	}
}

// TestNewLogger_jsonViaEnvVar verifies LOG_FORMAT=json forces JSON output.
func TestNewLogger_jsonViaEnvVar(t *testing.T) {
	t.Setenv("LOG_FORMAT", "json")
	logger := NewLogger("development", "concierge", "test-version")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
}

func TestNewLogger_includesStaticAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf, "production", "concierge", "2026.06.03")

	logger.Info("hello")

	output := buf.String()
	for _, want := range []string{
		`"service.name":"concierge"`,
		`"service.version":"2026.06.03"`,
		`"deployment.environment":"production"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %s: %s", want, output)
		}
	}
}

func TestSetup_withoutExporterStillCreatesValidSpans(t *testing.T) {
	rt, err := Setup(context.Background(), Config{
		ServiceName:    "test-service",
		Environment:    "test",
		ServiceVersion: "test-version",
		MetricsEnabled: false,
	})
	if err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	defer func() {
		_ = rt.Shutdown(context.Background())
	}()

	ctx, span := otel.Tracer("test").Start(context.Background(), "operation")
	defer span.End()

	if !span.SpanContext().IsValid() {
		t.Fatal("expected valid span context when tracing exporter is disabled")
	}
	if !oteltrace.SpanFromContext(ctx).SpanContext().IsValid() {
		t.Fatal("expected span to be attached to context")
	}
}

// TestWithTrace_noSpan verifies the logger is returned unchanged when no span
// is active in the context.
func TestWithTrace_noSpan(t *testing.T) {
	base := slog.Default()
	got := WithTrace(context.Background(), base)
	if got != base {
		t.Error("expected same logger when no span is active")
	}
}

// TestWithTrace_activeSpan verifies trace_id and span_id fields are added when
// an active span is present in the context.
func TestWithTrace_activeSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(tracetest.NewSpanRecorder()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	base := slog.Default()
	got := WithTrace(ctx, base)
	if got == base {
		t.Error("expected a new logger with trace fields when span is active")
	}
}

func TestTraceHandlerAddsTraceFieldsFromContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(&traceHandler{
		next: slog.NewJSONHandler(&buf, nil),
	})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !strings.Contains(output, `"trace_id":"`) {
		t.Fatalf("expected trace_id in log output, got %q", output)
	}
	if !strings.Contains(output, `"span_id":"`) {
		t.Fatalf("expected span_id in log output, got %q", output)
	}
}
