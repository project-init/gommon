//go:build unit_test

package mobilecompat

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHeaders(t *testing.T) {
	tests := []struct {
		name          string
		headers       http.Header
		expected      *Client
		errorContains string
	}{
		{
			name:    "no mobile headers",
			headers: http.Header{},
		},
		{
			name: "valid iOS headers",
			headers: http.Header{
				HeaderPlatform: []string{"ios"},
				HeaderVersion:  []string{"1.2.0"},
				HeaderBuild:    []string{"15"},
			},
			expected: &Client{
				Platform: PlatformIOS,
				Version:  "1.2.0",
				Build:    15,
			},
		},
		{
			name: "valid Android headers",
			headers: http.Header{
				HeaderPlatform: []string{"android"},
				HeaderVersion:  []string{"2.0.0"},
				HeaderBuild:    []string{"200"},
			},
			expected: &Client{
				Platform: PlatformAndroid,
				Version:  "2.0.0",
				Build:    200,
			},
		},
		{
			name: "incomplete headers",
			headers: http.Header{
				HeaderPlatform: []string{"ios"},
				HeaderVersion:  []string{"1.2.0"},
			},
			errorContains: "must be provided together",
		},
		{
			name: "unknown platform",
			headers: http.Header{
				HeaderPlatform: []string{"windows"},
				HeaderVersion:  []string{"1.2.0"},
				HeaderBuild:    []string{"15"},
			},
			errorContains: "must be \"ios\" or \"android\"",
		},
		{
			name: "invalid version",
			headers: http.Header{
				HeaderPlatform: []string{"ios"},
				HeaderVersion:  []string{"1.2"},
				HeaderBuild:    []string{"15"},
			},
			errorContains: HeaderVersion,
		},
		{
			name: "non-numeric build",
			headers: http.Header{
				HeaderPlatform: []string{"ios"},
				HeaderVersion:  []string{"1.2.0"},
				HeaderBuild:    []string{"abc"},
			},
			errorContains: "positive integer",
		},
		{
			name: "zero build",
			headers: http.Header{
				HeaderPlatform: []string{"ios"},
				HeaderVersion:  []string{"1.2.0"},
				HeaderBuild:    []string{"0"},
			},
			errorContains: "greater than zero",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := ParseHeaders(test.headers)

			if test.errorContains != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, test.errorContains)
				assert.Nil(t, actual)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}
