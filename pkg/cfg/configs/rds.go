package configs

// RDS Struct meant to wrap configuration values for terraform-aws-rds module
//
// * https://github.com/project-init/terraform-aws-rds
//

type RDS struct {
	PostgresDatabase string `env:"POSTGRES_DATABASE" yaml:"postgresDatabase" safe:"true"`
	PostgresPort     string `env:"POSTGRES_PORT" yaml:"postgresPort" safe:"true"`

	PostgresReadOnlyHost     string `env:"POSTGRES_READ_ONLY_HOST" yaml:"postgresReadOnlyHost" safe:"true"`
	PostgresReadOnlyUser     string `env:"POSTGRES_READ_ONLY_USER" yaml:"postgresReadOnlyUser" safe:"true"`
	PostgresReadOnlyPassword string `env:"POSTGRES_READ_ONLY_PASSWORD" yaml:"postgresReadOnlyPassword"`

	PostgresWriterHost     string `env:"POSTGRES_WRITER_HOST" yaml:"postgresWriterHost" safe:"true"`
	PostgresWriterUser     string `env:"POSTGRES_WRITER_USER" yaml:"postgresWriterUser" safe:"true"`
	PostgresWriterPassword string `env:"POSTGRES_WRITER_PASSWORD" yaml:"postgresWriterPassword"`
}
