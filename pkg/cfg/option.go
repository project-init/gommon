package cfg

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/ilyakaznacheev/cleanenv"
)

type Option interface {
	Apply(any) error
}

type FileOption struct {
	paths []string
}

func NewFileOption(paths ...string) Option {
	return &FileOption{
		paths: paths,
	}
}

func (o *FileOption) Apply(cfg any) error {
	for _, path := range o.paths {
		if err := cleanenv.ReadConfig(path, cfg); err != nil {
			return fmt.Errorf("read config file %s: %w", path, err)
		}
	}
	return nil
}

type EnvOption struct{}

func NewEnvOption() Option {
	return &EnvOption{}
}

func (o *EnvOption) Apply(cfg any) error {
	if err := cleanenv.ReadEnv(cfg); err != nil {
		return fmt.Errorf("read environment variables: %w", err)
	}
	return nil
}

type AWSSecretsManagerClient interface {
	GetSecretValue(ctx context.Context, input *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

type AWSSecretsManagerOption struct {
	client      AWSSecretsManagerClient
	secretNames []string
}

func NewAWSSecretsManagerOption(client AWSSecretsManagerClient, secretNames ...string) Option {
	return &AWSSecretsManagerOption{
		client:      client,
		secretNames: secretNames,
	}
}

func (o *AWSSecretsManagerOption) Apply(cfg any) error {
	for _, name := range o.secretNames {
		input := &secretsmanager.GetSecretValueInput{SecretId: &name}
		output, err := o.client.GetSecretValue(context.Background(), input)
		if err != nil {
			return fmt.Errorf("get secret value %s: %w", name, err)
		}

		if output.SecretString == nil {
			return fmt.Errorf("secret string is nil")
		}

		if err := cleanenv.ParseJSON(bytes.NewReader([]byte(*output.SecretString)), cfg); err != nil {
			return fmt.Errorf("read secret manager json value %s: %w", name, err)
		}
	}
	return nil
}
