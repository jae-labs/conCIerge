package observability

import (
	"context"
	"log/slog"
	"net/http"
)

// Runtime bundles application-facing observability dependencies and lifecycle.
type Runtime struct {
	Logger         *slog.Logger
	MetricsHandler http.Handler
	Shutdown       func(context.Context) error
}
