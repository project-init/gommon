package cfg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type innerConfig struct {
	SafeField   string `yaml:"safeField" safe:"true"`
	UnsafeField string `yaml:"unsafeField"`
}

type testConfig struct {
	SafeWithValue    string      `yaml:"safeWithValue" safe:"true"`
	SafeWithoutValue string      `yaml:"safeWithoutValue" safe:"true"`
	UnsafeWithValue  string      `yaml:"unsafeWithValue"`
	UnsafeNoValue    string      `yaml:"unsafeNoValue"`
	SafeSubConfig    innerConfig `yaml:"safeSubConfig" safe:"true"`
	UnsafeSubConfig  innerConfig `yaml:"unsafeSubConfig"`
	SafeIntWithValue int         `yaml:"safeIntWithValue" safe:"true"`
	SafeIntZero      int         `yaml:"safeIntZero" safe:"true"`
	UnsafeIntValue   int         `yaml:"unsafeIntValue"`
	UnsafeIntZero    int         `yaml:"unsafeIntZero"`
}

func TestMapAndRedact(t *testing.T) {
	cfg := testConfig{
		SafeWithValue:    "visible",
		UnsafeWithValue:  "secret",
		SafeIntWithValue: 42,
		SafeIntZero:      0,
		UnsafeIntValue:   99,
		UnsafeIntZero:    0,
		SafeSubConfig: innerConfig{
			SafeField:   "sub-visible",
			UnsafeField: "sub-secret",
		},
		UnsafeSubConfig: innerConfig{
			SafeField:   "should-not-appear",
			UnsafeField: "should-not-appear",
		},
	}

	result := MapAndRedact(cfg)

	// safe has value — included as-is
	assert.Equal(t, "visible", result["safeWithValue"])

	// safe doesn't have value (empty string) — omitted entirely
	assert.NotContains(t, result, "safeWithoutValue")

	// unsafe has value — redacted
	assert.Equal(t, "redacted", result["unsafeWithValue"])

	// unsafe doesn't have value — still redacted
	assert.Equal(t, "redacted", result["unsafeNoValue"])

	// safe int with non-zero value — included
	assert.Equal(t, 42, result["safeIntWithValue"])

	// safe int with zero value — still included (0 is a valid config value)
	assert.Equal(t, 0, result["safeIntZero"])

	// unsafe int with value — redacted
	assert.Equal(t, "redacted", result["unsafeIntValue"])

	// unsafe int with zero value — still redacted
	assert.Equal(t, "redacted", result["unsafeIntZero"])

	// safe sub-config: recurses, inner safe field visible, inner unsafe field redacted
	subSafe, ok := result["safeSubConfig"].(map[string]any)
	assert.True(t, ok, "safeSubConfig should be a map")
	assert.Equal(t, "sub-visible", subSafe["safeField"])
	assert.Equal(t, "redacted", subSafe["unsafeField"])

	// unsafe sub-config: whole thing is redacted, not recursed
	assert.Equal(t, "redacted", result["unsafeSubConfig"])
}
