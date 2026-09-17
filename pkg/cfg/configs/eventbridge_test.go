package configs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/project-init/gommon/pkg/cfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEventBridgeConfig struct {
	EventBridge EventBridge `yaml:"eventbridge" safe:"true"`
}

func TestEventBridgeDefaults(t *testing.T) {
	c := testEventBridgeConfig{}

	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, c.EventBridge.Timeout)
	assert.Equal(t, 3, c.EventBridge.MaxRetryAttempts)
}

func TestEventBridgeOverridesDefaults(t *testing.T) {
	c := testEventBridgeConfig{}

	t.Setenv("EVENT_BRIDGE_TIMEOUT", "10s")
	t.Setenv("EVENT_BRIDGE_MAX_RETRY_ATTEMPTS", "5")
	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, c.EventBridge.Timeout)
	assert.Equal(t, 5, c.EventBridge.MaxRetryAttempts)
}

func TestNewFileOptionForEventBridge(t *testing.T) {
	yamlContent := "eventbridge:\n  timeout: 10s\n  maxRetryAttempts: 5\n"

	c := testEventBridgeConfig{}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	writeFileErr := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, writeFileErr)

	err := cfg.LoadConfigs(&c, cfg.NewFileOption(path))
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, c.EventBridge.Timeout)
	assert.Equal(t, 5, c.EventBridge.MaxRetryAttempts)
}

func TestEventBridgeEventBusName(t *testing.T) {
	c := testEventBridgeConfig{}

	t.Setenv("EVENT_BRIDGE_EVENT_BUS_NAME", "my-bus")
	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)
	assert.Equal(t, "my-bus", c.EventBridge.EventBusName)
}
