package log

import (
	"context"
	"log/slog"
	"os"

	slogcontext "github.com/veqryn/slog-context"
)

type Config struct {
	Level        slog.Level
	AddSource    bool
	DefaultAttrs map[string]any
}

// Set sets slog default logger based on the provided configuration.
func Set(cfg *Config) {
	handlerOpts := &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: cfg.AddSource,
	}

	handler := slog.NewJSONHandler(os.Stdout, handlerOpts)
	l := slog.New(handler)

	// Add custom attributes
	for key, value := range cfg.DefaultAttrs {
		l = l.With(key, value)
	}

	slog.SetDefault(l)
}

// Get returns the default slog logger. You can also use slog.Default() directly.
func Get() *slog.Logger {
	return slog.Default()
}

// GetOverridden returns a new slog logger that overrides the log level to the given level.
// This is useful when you want to temporarily change the log level for specific operations.
func GetOverridden(lvl slog.Level) *slog.Logger {
	return slog.New(newLevelHandler(slog.LevelDebug, Get().Handler()))
}

// NewContext returns a new context with the provided slog logger. Useful for passing loggers through contexts.
func NewContext(ctx context.Context, l *slog.Logger) context.Context {
	return slogcontext.NewCtx(ctx, l)
}

// FromContext retrieves the slog logger from the context. If no logger is found, it returns the default logger.
func FromContext(ctx context.Context) *slog.Logger {
	return slogcontext.FromCtx(ctx)
}
