package configs

import "github.com/project-init/gommon/pkg/loglevel"

// Logging configuration struct for use across services
//
// Usage example:
//
//	type Config struct {
//		ServiceConfig configs.Service `env:"SERVICE" yaml:"service"`
//		LoggingConfig configs.Logging `env:"LOGGING" yaml:"logging"`
//	}
//
//	config := GetConfig()
//	sre.InitializeLogger(config.ServiceConfig.Env, config.LoggingConfig.Level.ToSlog())
type Logging struct {
	Level loglevel.Level `env:"LEVEL" yaml:"level" envDefault:"info"`
}
