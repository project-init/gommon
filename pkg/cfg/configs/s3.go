package configs

// S3 Struct meant to wrap configuration values to work alongside the https://github.com/project-init/terraform-aws-s3
// terraform module.
// Note: env-prefix:"S3_" combines with the field's env:"BUCKET" to read S3_BUCKET, and a bare BUCKET will be ignored.
// Usage would look similar to
//
//	type Config struct {
//		// S3
//		S3Config configs.S3 `env-prefix:"S3_" yaml:"s3" safe:"true"`
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
type S3 struct {
	Bucket string `env:"BUCKET" yaml:"bucket" safe:"true"`
}
