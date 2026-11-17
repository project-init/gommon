package configs

import "time"

// Jira Struct meant to wrap the configuration for a Jira Cloud client, such as jiraclient.Client.
// With Email set the client uses Basic auth against the site URL (https://<site>.atlassian.net).
// Without it the token is sent as a Bearer token, as Atlassian service-account tokens require on
// https://api.atlassian.com/ex/jira/<cloudId>.
//
// Embed it, rather than naming the field, so the token is read from a flat Secrets Manager value
// such as {"JIRA_READ_ONLY_API_KEY": "..."}. Usage would look similar to
//
//	type Config struct {
//		// Jira
//		configs.Jira `yaml:"jira" safe:"true"`
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
type Jira struct {
	BaseURL  string        `env:"JIRA_BASE_URL" yaml:"baseUrl" safe:"true"`
	Email    string        `env:"JIRA_EMAIL" yaml:"email" safe:"true"`
	APIKey string        `env:"JIRA_API_KEY" yaml:"apiKey" safe:"false"`
	PageSize int           `env:"JIRA_PAGE_SIZE" yaml:"pageSize" env-default:"100" safe:"true"`
	Timeout  time.Duration `env:"JIRA_TIMEOUT" yaml:"timeout" env-default:"30s" safe:"true"`
}
