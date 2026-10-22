package configs

// RDS Struct meant to wrap configuration values for terraform-aws-rds module
//
// * https://github.com/project-init/terraform-aws-rds
//
//	Usage would look similar to
//
//	type Config struct {
//		// Postgres
//		RdsConfig configs.RDS `env:"POSTGRES" yaml:"postgres" safe:"true"`
//	}
//
//	var (
//		configuration *Config = &Config{}
//		once          sync.Once
//	)
//
//	func GetConfig() *Config {
//		once.Do(func() {
//			opts := []cfg.Option{}
//
//			if configFiles, ok := os.LookupEnv(EnvVarConfigFiles); ok && len(configFiles) > 0 {
//				opts = append(opts, cfg.NewFileOption(configFiles))
//			}
//
//			if smSecrets, ok := os.LookupEnv(EnvVarConfigAWSSecretsManagerSecrets); ok && len(smSecrets) > 0 {
//				opts = append(opts, cfg.NewAWSSecretsManagerOption(secretsmanager.NewFromConfig(aws.GetConfig()), strings.Split(smSecrets, ",")...))
//			}
//
//			opts = append(opts, cfg.NewEnvOption())
//
//			if err := cfg.LoadConfigs(configuration, opts...); err != nil {
//				sre.LogFatal(err)
//			}
//		})
//
//		return configuration
//	}

type RDS struct {
	PostgresDatabase string `env:"POSTGRES_DATABASE" yaml:"postgresDatabase" safe:"true"`
	PostgresPort     string `env:"POSTGRES_PORT" yaml:"postgresPort" safe:"true"`

	PostgresReadOnlyHost            string `env:"POSTGRES_READ_ONLY_HOST" yaml:"postgresReadOnlyHost" safe:"true"`
	PostgresReadOnlyUser            string `env:"POSTGRES_READ_ONLY_USER" yaml:"postgresReadOnlyUser" safe:"true"`
	PostgresReadOnlyPassword        string `env:"POSTGRES_READ_ONLY_PASSWORD" yaml:"postgresReadOnlyPassword"`
	PostgresReadOnlyMinConnections  int    `env:"POSTGRES_READ_ONLY_MIN_CONNECTIONS" yaml:"postgresReadOnlyMinConnections" env-default:"1" safe:"true"`
	PostgresReadOnlyMaxConnections  int    `env:"POSTGRES_READ_ONLY_MAX_CONNECTIONS" yaml:"postgresReadOnlyMaxConnections" env-default:"10" safe:"true"`
	PostgresReadOnlyMaxConnLifetime string `env:"POSTGRES_READ_ONLY_MAX_CONN_LIFETIME" yaml:"postgresReadOnlyMaxConnLifetime" env-default:"14m" safe:"true"`
	PostgresReadOnlyMaxConnIdleTime string `env:"POSTGRES_READ_ONLY_MAX_CONN_IDLE_TIME" yaml:"postgresReadOnlyMaxConnIdleTime" env-default:"14m" safe:"true"`

	PostgresWriterHost            string `env:"POSTGRES_WRITER_HOST" yaml:"postgresWriterHost" safe:"true"`
	PostgresWriterUser            string `env:"POSTGRES_WRITER_USER" yaml:"postgresWriterUser" safe:"true"`
	PostgresWriterPassword        string `env:"POSTGRES_WRITER_PASSWORD" yaml:"postgresWriterPassword"`
	PostgresWriterMinConnections  int    `env:"POSTGRES_WRITER_MIN_CONNECTIONS" yaml:"postgresWriterMinConnections" env-default:"1" safe:"true"`
	PostgresWriterMaxConnections  int    `env:"POSTGRES_WRITER_MAX_CONNECTIONS" yaml:"postgresWriterMaxConnections" env-default:"10" safe:"true"`
	PostgresWriterMaxConnLifetime string `env:"POSTGRES_WRITER_MAX_CONN_LIFETIME" yaml:"postgresWriterMaxConnLifetime" env-default:"14m" safe:"true"`
	PostgresWriterMaxConnIdleTime string `env:"POSTGRES_WRITER_MAX_CONN_IDLE_TIME" yaml:"postgresWriterMaxConnIdleTime" env-default:"14m" safe:"true"`
}
