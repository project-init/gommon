package configs

// Kafka wraps configuration values for Kafka consumer/producer connectivity.
//
// Brokers and Topics are comma-separated strings to work with environment variables
// and the flat config pattern used by other config structs in this package.
//
//	type Config struct {
//		Kafka configs.Kafka `env:"KAFKA" yaml:"kafka" safe:"true"`
//	}
type Kafka struct {
	Brokers       string `env:"KAFKA_BROKERS" yaml:"brokers" safe:"true"`
	GroupID       string `env:"KAFKA_GROUP_ID" yaml:"groupId" safe:"true"`
	Topics        string `env:"KAFKA_TOPICS" yaml:"topics" safe:"true"`
	SASLMechanism string `env:"KAFKA_SASL_MECHANISM" yaml:"saslMechanism" safe:"true"`
	SASLUsername  string `env:"KAFKA_SASL_USERNAME" yaml:"saslUsername" safe:"true"`
	SASLPassword  string `env:"KAFKA_SASL_PASSWORD" yaml:"saslPassword"`
	TLSEnabled    bool   `env:"KAFKA_TLS_ENABLED" yaml:"tlsEnabled" safe:"true"`
}
