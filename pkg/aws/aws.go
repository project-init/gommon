package aws

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

var (
	cfg  aws.Config
	once sync.Once
)

func GetConfig() aws.Config {
	once.Do(func() {
		dcfg, err := config.LoadDefaultConfig(context.Background())
		if err != nil {
			panic(err)
		}

		cfg = dcfg
	})

	return cfg
}
