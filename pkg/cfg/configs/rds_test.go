package configs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/project-init/gommon/pkg/cfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRDSConfig struct {
	RDS RDS `yaml:"postgres" safe:"true"`
}

func TestRDSDefaults(t *testing.T) {
	c := testRDSConfig{}

	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)

	assert.Equal(t, 1, c.RDS.PostgresReadOnlyMinConnections)
	assert.Equal(t, 10, c.RDS.PostgresReadOnlyMaxConnections)
	assert.Equal(t, "14m", c.RDS.PostgresReadOnlyMaxConnLifetime)
	assert.Equal(t, "14m", c.RDS.PostgresReadOnlyMaxConnIdleTime)

	assert.Equal(t, 1, c.RDS.PostgresWriterMinConnections)
	assert.Equal(t, 10, c.RDS.PostgresWriterMaxConnections)
	assert.Equal(t, "14m", c.RDS.PostgresWriterMaxConnLifetime)
	assert.Equal(t, "14m", c.RDS.PostgresWriterMaxConnIdleTime)
}

func TestRDSOverridesDefaults(t *testing.T) {
	c := testRDSConfig{}

	t.Setenv("POSTGRES_READ_ONLY_MIN_CONNECTIONS", "2")
	t.Setenv("POSTGRES_READ_ONLY_MAX_CONNECTIONS", "25")
	t.Setenv("POSTGRES_READ_ONLY_MAX_CONN_LIFETIME", "30m")
	t.Setenv("POSTGRES_READ_ONLY_MAX_CONN_IDLE_TIME", "5m")
	t.Setenv("POSTGRES_WRITER_MIN_CONNECTIONS", "3")
	t.Setenv("POSTGRES_WRITER_MAX_CONNECTIONS", "50")
	t.Setenv("POSTGRES_WRITER_MAX_CONN_LIFETIME", "1h")
	t.Setenv("POSTGRES_WRITER_MAX_CONN_IDLE_TIME", "10m")

	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)

	assert.Equal(t, 2, c.RDS.PostgresReadOnlyMinConnections)
	assert.Equal(t, 25, c.RDS.PostgresReadOnlyMaxConnections)
	assert.Equal(t, "30m", c.RDS.PostgresReadOnlyMaxConnLifetime)
	assert.Equal(t, "5m", c.RDS.PostgresReadOnlyMaxConnIdleTime)

	assert.Equal(t, 3, c.RDS.PostgresWriterMinConnections)
	assert.Equal(t, 50, c.RDS.PostgresWriterMaxConnections)
	assert.Equal(t, "1h", c.RDS.PostgresWriterMaxConnLifetime)
	assert.Equal(t, "10m", c.RDS.PostgresWriterMaxConnIdleTime)
}

func TestRDSNewEnvOption(t *testing.T) {
	c := testRDSConfig{}

	t.Setenv("POSTGRES_DATABASE", "my-database")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_READ_ONLY_HOST", "read-only-host")
	t.Setenv("POSTGRES_READ_ONLY_USER", "read-only-user")
	t.Setenv("POSTGRES_READ_ONLY_PASSWORD", "read-only-password")
	t.Setenv("POSTGRES_WRITER_HOST", "writer-host")
	t.Setenv("POSTGRES_WRITER_USER", "writer-user")
	t.Setenv("POSTGRES_WRITER_PASSWORD", "writer-password")

	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.NoError(t, err)

	assert.Equal(t, "my-database", c.RDS.PostgresDatabase)
	assert.Equal(t, "5432", c.RDS.PostgresPort)
	assert.Equal(t, "read-only-host", c.RDS.PostgresReadOnlyHost)
	assert.Equal(t, "read-only-user", c.RDS.PostgresReadOnlyUser)
	assert.Equal(t, "read-only-password", c.RDS.PostgresReadOnlyPassword)
	assert.Equal(t, "writer-host", c.RDS.PostgresWriterHost)
	assert.Equal(t, "writer-user", c.RDS.PostgresWriterUser)
	assert.Equal(t, "writer-password", c.RDS.PostgresWriterPassword)
}

func TestRDSNewEnvOptionWithInvalidConnectionCount(t *testing.T) {
	c := testRDSConfig{}

	t.Setenv("POSTGRES_WRITER_MAX_CONNECTIONS", "not-a-number")

	err := cfg.LoadConfigs(&c, cfg.NewEnvOption())
	require.Error(t, err)
}

func TestRDSNewFileOption(t *testing.T) {
	yamlContent := `postgres:
  postgresDatabase: my-database
  postgresPort: "5432"
  postgresReadOnlyHost: read-only-host
  postgresReadOnlyUser: read-only-user
  postgresReadOnlyPassword: read-only-password
  postgresReadOnlyMinConnections: 2
  postgresReadOnlyMaxConnections: 25
  postgresReadOnlyMaxConnLifetime: 30m
  postgresReadOnlyMaxConnIdleTime: 5m
  postgresWriterHost: writer-host
  postgresWriterUser: writer-user
  postgresWriterPassword: writer-password
  postgresWriterMinConnections: 3
  postgresWriterMaxConnections: 50
  postgresWriterMaxConnLifetime: 1h
  postgresWriterMaxConnIdleTime: 10m
`

	c := testRDSConfig{}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	writeFileErr := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, writeFileErr)

	err := cfg.LoadConfigs(&c, cfg.NewFileOption(path))
	require.NoError(t, err)

	assert.Equal(t, "my-database", c.RDS.PostgresDatabase)
	assert.Equal(t, "5432", c.RDS.PostgresPort)
	assert.Equal(t, "read-only-host", c.RDS.PostgresReadOnlyHost)
	assert.Equal(t, "read-only-user", c.RDS.PostgresReadOnlyUser)
	assert.Equal(t, "read-only-password", c.RDS.PostgresReadOnlyPassword)
	assert.Equal(t, 2, c.RDS.PostgresReadOnlyMinConnections)
	assert.Equal(t, 25, c.RDS.PostgresReadOnlyMaxConnections)
	assert.Equal(t, "30m", c.RDS.PostgresReadOnlyMaxConnLifetime)
	assert.Equal(t, "5m", c.RDS.PostgresReadOnlyMaxConnIdleTime)
	assert.Equal(t, "writer-host", c.RDS.PostgresWriterHost)
	assert.Equal(t, "writer-user", c.RDS.PostgresWriterUser)
	assert.Equal(t, "writer-password", c.RDS.PostgresWriterPassword)
	assert.Equal(t, 3, c.RDS.PostgresWriterMinConnections)
	assert.Equal(t, 50, c.RDS.PostgresWriterMaxConnections)
	assert.Equal(t, "1h", c.RDS.PostgresWriterMaxConnLifetime)
	assert.Equal(t, "10m", c.RDS.PostgresWriterMaxConnIdleTime)
}

func TestRDSNewFileOptionAppliesDefaults(t *testing.T) {
	yamlContent := "postgres:\n  postgresDatabase: my-database\n"

	c := testRDSConfig{}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	writeFileErr := os.WriteFile(path, []byte(yamlContent), 0644)
	require.NoError(t, writeFileErr)

	err := cfg.LoadConfigs(&c, cfg.NewFileOption(path))
	require.NoError(t, err)

	assert.Equal(t, "my-database", c.RDS.PostgresDatabase)
	assert.Equal(t, 1, c.RDS.PostgresReadOnlyMinConnections)
	assert.Equal(t, 10, c.RDS.PostgresReadOnlyMaxConnections)
	assert.Equal(t, 1, c.RDS.PostgresWriterMinConnections)
	assert.Equal(t, 10, c.RDS.PostgresWriterMaxConnections)
}
