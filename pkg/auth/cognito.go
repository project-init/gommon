package auth

// CognitoConfig Struct meant to wrap configuration values to work alongside the https://github.com/project-init/terraform-aws-cognito
// terraform module. Usage would look similar to
//
//	type Config struct {
//		// Cognito
//		CognitoConfig auth.CognitoConfig `env:"COGNITO" yaml:"cognito" safe:"true"`
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
type CognitoConfig struct {
	UseLocal   bool   `env:"COGNITO_USE_LOCAL" yaml:"cognitoUseLocal" envDefault:"false" safe:"true"`
	Endpoint   string `env:"COGNITO_ENDPOINT" yaml:"cognitoEndpoint" safe:"true"`
	ClientId   string `env:"COGNITO_CLIENT_ID" json:"cognitoClientId" yaml:"cognitoClientId"`
	UserPoolId string `env:"COGNITO_USER_POOL_ID" json:"cognitoUserPoolId" yaml:"cognitoUserPoolId"`
}
