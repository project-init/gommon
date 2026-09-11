package configs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/project-init/gommon/pkg/cfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testS3Config struct {
	S3 S3 `env-prefix:"S3_" yaml:"s3" safe:"true"`
}

func TestNewEnvOptionWithPrefix(t *testing.T) {
	c := testS3Config{}

	t.Setenv("S3_BUCKET", "my-bucket")
	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)
	assert.Equal(t, "my-bucket", c.S3.Bucket)
}

func TestNewEnvOptionWithoutPrefix(t *testing.T) {
	c := testS3Config{}

	t.Setenv("BUCKET", "my-bucket")
	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)
	assert.Empty(t, c.S3.Bucket)
}

func TestNewFileOption(t *testing.T) {
	yamlContent := "s3:\n  bucket:  my-bucket\n"

	c := testS3Config{}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	writeFileErr := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, writeFileErr)

	err := cfg.LoadConfigs(&c, cfg.NewFileOption(path))
	require.NoError(t, err)
	assert.Equal(t, "my-bucket", c.S3.Bucket)
}
