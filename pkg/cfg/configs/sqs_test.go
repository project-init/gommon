package configs

import (
	"testing"
	"time"

	"github.com/project-init/gommon/pkg/cfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testSQSConfig struct {
	SQS SQS `yaml:"sqs" safe:"true"`
}

func TestSQSDefaults(t *testing.T) {
	c := testSQSConfig{}

	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)
	assert.Equal(t, 3*time.Minute, c.SQS.HeartbeatInterval)
	assert.Equal(t, 6*time.Minute, c.SQS.HeartbeatExtension)
	assert.Equal(t, 4, c.SQS.Concurrency)
}

func TestSQSHeartbeatFromEnv(t *testing.T) {
	c := testSQSConfig{}

	t.Setenv("SQS_WORKER_HEARTBEAT_INTERVAL", "20s")
	t.Setenv("SQS_WORKER_HEARTBEAT_EXTENSION", "1m")
	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)
	assert.Equal(t, 20*time.Second, c.SQS.HeartbeatInterval)
	assert.Equal(t, time.Minute, c.SQS.HeartbeatExtension)
}
