//go:build unit_test

package mobilecompat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIOSBelowMinimum(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		currentBuild   int64
		minimumVersion string
		minimumBuild   int64
		expected       bool
	}{
		{
			name:           "release below minimum",
			currentVersion: "1.1.0",
			currentBuild:   100,
			minimumVersion: "1.2.0",
			minimumBuild:   1,
			expected:       true,
		},
		{
			name:           "release above minimum ignores build",
			currentVersion: "1.3.0",
			currentBuild:   1,
			minimumVersion: "1.2.0",
			minimumBuild:   100,
			expected:       false,
		},
		{
			name:           "same release below minimum build",
			currentVersion: "1.2.0",
			currentBuild:   9,
			minimumVersion: "1.2.0",
			minimumBuild:   10,
			expected:       true,
		},
		{
			name:           "exact minimum",
			currentVersion: "1.2.0",
			currentBuild:   10,
			minimumVersion: "1.2.0",
			minimumBuild:   10,
			expected:       false,
		},
		{
			name:           "same release above minimum build",
			currentVersion: "1.2.0",
			currentBuild:   11,
			minimumVersion: "1.2.0",
			minimumBuild:   10,
			expected:       false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := IOSBelowMinimum(
				test.currentVersion,
				test.currentBuild,
				test.minimumVersion,
				test.minimumBuild,
			)

			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestIOSBelowMinimumRejectsInvalidVersion(t *testing.T) {
	_, err := IOSBelowMinimum("invalid", 1, "1.2.0", 1)

	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid current version")
}

func TestAndroidBelowMinimum(t *testing.T) {
	tests := []struct {
		name        string
		currentCode int64
		minimumCode int64
		expected    bool
	}{
		{
			name:        "below minimum",
			currentCode: 99,
			minimumCode: 100,
			expected:    true,
		},
		{
			name:        "exact minimum",
			currentCode: 100,
			minimumCode: 100,
			expected:    false,
		},
		{
			name:        "above minimum",
			currentCode: 101,
			minimumCode: 100,
			expected:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := AndroidBelowMinimum(
				test.currentCode,
				test.minimumCode,
			)

			assert.Equal(t, test.expected, actual)
		})
	}
}
