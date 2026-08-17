package configs

import "time"

// EventBridge Struct meant to wrap configuration values to work alongside the https://github.com/project-init/terraform-aws-event-bridge
// terraform module. Usage would look similar to
//
//	type Config struct {
//		// EventBridge
//		EventBridgeConfig cfg.EventBridgeConfig `env:"EVENT_BRIDGE" yaml:"EventBridge" safe:"true"`
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
type EventBridge struct {
	EventBusName     string        `env:"EVENT_BUS_NAME" yaml:"eventBusName" safe:"true"`
	Timeout          time.Duration `env:"TIMEOUT" yaml:"timeout" safe:"true"`
	MaxRetryAttempts int           `env:"MAX_RETRY_ATTEMPTS" yaml:"maxRetryAttempts" safe:"true"`
}
