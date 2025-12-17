package cfg

import "fmt"

func LoadConfigs(cfg any, options ...Option) error {
	for _, opt := range options {
		if err := opt.Apply(cfg); err != nil {
			return fmt.Errorf("can't apply config option: %w", err)
		}
	}

	return nil
}
