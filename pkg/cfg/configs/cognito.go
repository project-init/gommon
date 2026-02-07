package configs

// Cognito Struct meant to wrap configuration values to work alongside the https://github.com/project-init/terraform-aws-cognito
// terraform module. Usage would look similar to
//
//	type Config struct {
//		// Cognito
//		CognitoConfig cfg.CognitoConfig `env:"COGNITO" yaml:"cognito" safe:"true"`
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
type Cognito struct {
	UseLocal   bool   `env:"COGNITO_USE_LOCAL" yaml:"cognitoUseLocal" envDefault:"false" safe:"true"`
	Endpoint   string `env:"COGNITO_ENDPOINT" yaml:"cognitoEndpoint" safe:"true"`
	ClientId   string `env:"COGNITO_CLIENT_ID" json:"cognitoClientId" yaml:"cognitoClientId"`
	UserPoolId string `env:"COGNITO_USER_POOL_ID" json:"cognitoUserPoolId" yaml:"cognitoUserPoolId"`
	IssuerUrl  string `env:"COGNITO_ISSUER_URL" yaml:"cognitoIssuerUrl"`

	// This will need to be set by the user as it will be dependent on your setup. For example, you might have multiple
	// callback urls that are valid, but if you are running against a live, prelive, or local environment the redirect
	// will be different. Therefore, this is not set by the terraform module itself, but instead should be added by the
	// user.
	RedirectUrl string `env:"COGNITO_REDIRECT_URL" yaml:"cognitoRedirectUrl"`
}
