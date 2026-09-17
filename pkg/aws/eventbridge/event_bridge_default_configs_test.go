package eventbridge

import (
	"testing"

	"github.com/project-init/gommon/pkg/cfg"
	"github.com/project-init/gommon/pkg/cfg/configs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The env-default tags on configs.EventBridge can't reference these constants,
// so this keeps the two in sync.
func TestDefaultsMatchConfigEnvDefaults(t *testing.T) {
	var c struct {
		EventBridge configs.EventBridge `yaml:"eventbridge"`
	}

	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)
	assert.Equal(t, defaultPutEventsTimeout, c.EventBridge.Timeout)
	assert.Equal(t, defaultMaxRetryAttempts, c.EventBridge.MaxRetryAttempts)
}
