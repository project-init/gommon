package configs

import "time"

// SQS Struct meant to wrap configuration values to work alongside the https://github.com/project-init/terraform-aws-sqs
// terraform module. Usage would look similar to
//
//	type Config struct {
//		// SQS
//		SQSConfig configs.SQS `env:"SQS" yaml:"sqs" safe:"true"`
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

type SQS struct {
	QueueURL string `env:"SQS_QUEUE_URL" yaml:"queueUrl" safe:"true"`
	// HeartbeatInterval and HeartbeatExtension feed sqs.WithHeartbeat: every interval, the worker keeps each
	// message it holds hidden for extension from then. The defaults match the worker's.
	HeartbeatInterval  time.Duration `env:"SQS_WORKER_HEARTBEAT_INTERVAL" yaml:"workerHeartbeatInterval" env-default:"3m" safe:"true"`
	HeartbeatExtension time.Duration `env:"SQS_WORKER_HEARTBEAT_EXTENSION" yaml:"workerHeartbeatExtension" env-default:"6m" safe:"true"`
	Concurrency        int           `env:"SQS_WORKER_CONCURRENCY" yaml:"workerConcurrency" env-default:"4" safe:"true"`
}
