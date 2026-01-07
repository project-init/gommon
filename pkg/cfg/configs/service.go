package configs

import "github.com/project-init/gommon/pkg/env"

// Service Struct meant to wrap configuration values to work alongside service modules from project-init
//
// * https://github.com/project-init/terraform-aws-api-service
// * https://github.com/project-init/terraform-aws-cron
// * https://github.com/project-init/terraform-aws-grpc-service
//
// Usage would look similar to
//
//	type Config struct {
//		// Service
//		ServiceConfig cfg.ServiceConfig `yaml:"service" safe:"true"`
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
type Service struct {
	Env     env.Env `env:"ENV" yaml:"env" safe:"true"`
	Region  string  `env:"AWS_REGION" yaml:"region" safe:"true"`
	Version string  `env:"VERSION" yaml:"version" safe:"true"`
}
