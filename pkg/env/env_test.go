package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Environment(t *testing.T) {
	development := Env("development")
	staging := Env("staging")
	production := Env("production")

	assert.Equal(t, "development", development.String())
	assert.Equal(t, "staging", staging.String())
	assert.Equal(t, "production", production.String())

	assert.True(t, development.IsDevelopment())
	assert.False(t, staging.IsDevelopment())
	assert.False(t, production.IsDevelopment())
}
