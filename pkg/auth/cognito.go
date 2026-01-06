package auth

// CognitoConfig Struct meant to wrap configuration values to work alongside the https://github.com/project-init/terraform-aws-cognito
// terraform module. Usage would look similar to
//
//	type Config struct {
//		// Cognito
//		CognitoConfig auth.CognitoConfig `env:"COGNITO" yaml:"cognito" safe:"true"`
//	}
type CognitoConfig struct {
	UseLocal   bool   `env:"COGNITO_USE_LOCAL" yaml:"cognitoUseLocal" envDefault:"false" safe:"true"`
	Endpoint   string `env:"COGNITO_ENDPOINT" yaml:"cognitoEndpoint" safe:"true"`
	ClientId   string `env:"COGNITO_CLIENT_ID" json:"cognitoClientId" yaml:"cognitoClientId"`
	UserPoolId string `env:"COGNITO_USER_POOL_ID" json:"cognitoUserPoolId" yaml:"cognitoUserPoolId"`
}
