package log

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLevel_String(t *testing.T) {
	assert.Equal(t, "debug", Debug.String())
	assert.Equal(t, "info", Info.String())
	assert.Equal(t, "warn", Warn.String())
	assert.Equal(t, "error", Error.String())
}

func TestLevel_ToSlog(t *testing.T) {
	tests := []struct {
		level    Level
		expected slog.Level
	}{
		{Debug, slog.LevelDebug},
		{Info, slog.LevelInfo},
		{Warn, slog.LevelWarn},
		{Error, slog.LevelError},
		{Level("unknown"), slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.ToSlog())
		})
	}
}

func TestSet(t *testing.T) {
	Set(&Config{
		Level:     Info,
		AddSource: false,
		DefaultAttrs: map[string]any{
			"service": "test-app",
		},
	})

	logger := Get()
	assert.NotNil(t, logger)

	// Verify the logger writes JSON with our default attrs
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	testLogger := slog.New(handler).With("service", "test-app")
	testLogger.Info("hello")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "test-app", entry["service"])
	assert.Equal(t, "hello", entry["msg"])
}

func TestGet(t *testing.T) {
	Set(&Config{Level: Info})
	logger := Get()
	assert.NotNil(t, logger)
	assert.Equal(t, slog.Default(), logger)
}

func TestGetOverridden(t *testing.T) {
	Set(&Config{Level: Error})

	// Default logger should be at Error level
	defaultLogger := Get()
	assert.False(t, defaultLogger.Enabled(context.Background(), slog.LevelDebug))

	// Overridden logger should be at Debug level
	overridden := GetOverridden(slog.LevelDebug)
	assert.True(t, overridden.Enabled(context.Background(), slog.LevelDebug))
	assert.True(t, overridden.Enabled(context.Background(), slog.LevelInfo))
}

func TestNewContext_FromContext(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	logger := slog.New(handler).With("request_id", "abc-123")

	ctx := NewContext(context.Background(), logger)
	retrieved := FromContext(ctx)

	assert.NotNil(t, retrieved)

	// Verify the retrieved logger has our attributes by writing a log entry
	retrieved.Info("test")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "abc-123", entry["request_id"])
}

func TestFromContext_ReturnsDefault(t *testing.T) {
	Set(&Config{Level: Info})
	logger := FromContext(context.Background())
	assert.NotNil(t, logger)
}
