package loglevel

import (
	"log/slog"
	"testing"
)

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{Debug, "debug"},
		{Info, "info"},
		{Warn, "warn"},
		{Error, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.level.String(); got != tt.want {
				t.Errorf("Level.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLevel_ToSlog(t *testing.T) {
	tests := []struct {
		level Level
		want  slog.Level
	}{
		{Debug, slog.LevelDebug},
		{Info, slog.LevelInfo},
		{Warn, slog.LevelWarn},
		{Error, slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			if got := tt.level.ToSlog(); got != tt.want {
				t.Errorf("Level.ToSlog() = %v, want %v", got, tt.want)
			}
		})
	}
}
