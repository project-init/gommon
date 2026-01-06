package auth

type CognitoConfig struct {
	CognitoEndpoint   string `env:"COGNITO_ENDPOINT" yaml:"cognitoEndpoint" safe:"true"`
	CognitoClientId   string `env:"COGNITO_CLIENT_ID" json:"cognitoClientId" yaml:"cognitoClientId"`
	CognitoUserPoolId string `env:"COGNITO_USER_POOL_ID" json:"cognitoUserPoolId" yaml:"cognitoUserPoolId"`
}
