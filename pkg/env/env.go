package env

import (
	"fmt"
	"os"
)

type Env string

const (
	Development Env = "development"
	Staging     Env = "staging"
	Production  Env = "production"
)

func (e Env) String() string {
	return string(e)
}

func (e Env) IsDevelopment() bool {
	return e == Development
}

func (e Env) IsNonDevelopment() bool {
	return !e.IsDevelopment()
}

func Resolve() (Env, error) {
	switch Env(os.Getenv("ENV")) {
	case Development:
		return Development, nil
	case Staging:
		return Staging, nil
	case Production:
		return Production, nil
	}

	return "", fmt.Errorf("unknown environment: %s", os.Getenv("ENV"))
}
