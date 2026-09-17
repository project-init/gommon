package mobilecompat

import (
	"fmt"
	"net/http"
	"strconv"
)

const (
	HeaderPlatform = "X-App-Platform"
	HeaderVersion  = "X-App-Version"
	HeaderBuild    = "X-App-Build"

	HeaderUpdateScope    = "X-App-Update-Scope"
	HeaderMinimumVersion = "X-App-Minimum-Version"
	HeaderMinimumBuild   = "X-App-Minimum-Build"
	HeaderStoreURL       = "X-App-Store-URL"
)

type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
)

type Client struct {
	Platform Platform
	Version  string
	Build    int64
}

// ParseHeaders returns nil when no mobile headers are supplied.
// If any mobile header is present, all three are required.
func ParseHeaders(headers http.Header) (*Client, error) {
	platformValue := headers.Get(HeaderPlatform)
	versionValue := headers.Get(HeaderVersion)
	buildValue := headers.Get(HeaderBuild)

	if platformValue == "" && versionValue == "" && buildValue == "" {
		return nil, nil
	}

	if platformValue == "" || versionValue == "" || buildValue == "" {
		return nil, fmt.Errorf(
			"%s, %s, and %s must be provided together",
			HeaderPlatform,
			HeaderVersion,
			HeaderBuild,
		)
	}

	platform := Platform(platformValue)
	switch platform {
	case PlatformIOS, PlatformAndroid:
	default:
		return nil, fmt.Errorf(
			"%s must be %q or %q",
			HeaderPlatform,
			PlatformIOS,
			PlatformAndroid,
		)
	}

	if err := ValidateVersion(versionValue); err != nil {
		return nil, fmt.Errorf("%s: %w", HeaderVersion, err)
	}

	build, err := strconv.ParseInt(buildValue, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be a positive integer", HeaderBuild)
	}

	if build <= 0 {
		return nil, fmt.Errorf("%s must be greater than zero", HeaderBuild)
	}

	return &Client{
		Platform: platform,
		Version:  versionValue,
		Build:    build,
	}, nil
}
