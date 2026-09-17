package mobilecompat

import (
	"fmt"
	"strconv"
	"strings"
)

type releaseVersion struct {
	major uint64
	minor uint64
	patch uint64
}

func ValidateVersion(value string) error {
	_, err := parseVersion(value)
	return err
}

// IOSBelowMinimum compares the release version first. The build is compared
// only when the installed and minimum release versions are equal.
func IOSBelowMinimum(
	currentVersion string,
	currentBuild int64,
	minimumVersion string,
	minimumBuild int64,
) (bool, error) {
	current, err := parseVersion(currentVersion)
	if err != nil {
		return false, fmt.Errorf("invalid current version: %w", err)
	}

	minimum, err := parseVersion(minimumVersion)
	if err != nil {
		return false, fmt.Errorf("invalid minimum version: %w", err)
	}

	comparison := compareVersions(current, minimum)
	if comparison < 0 {
		return true, nil
	}

	if comparison > 0 {
		return false, nil
	}

	return currentBuild < minimumBuild, nil
}

// AndroidBelowMinimum compares versionCode, represented by the build header.
func AndroidBelowMinimum(currentVersionCode, minimumVersionCode int64) bool {
	return currentVersionCode < minimumVersionCode
}

func parseVersion(value string) (releaseVersion, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return releaseVersion{}, fmt.Errorf(
			"%q must use major.minor.patch format",
			value,
		)
	}

	values := make([]uint64, 3)
	for index, part := range parts {
		if part == "" {
			return releaseVersion{}, fmt.Errorf(
				"%q must use numeric major.minor.patch format",
				value,
			)
		}

		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return releaseVersion{}, fmt.Errorf(
				"%q must use numeric major.minor.patch format",
				value,
			)
		}

		values[index] = parsed
	}

	return releaseVersion{
		major: values[0],
		minor: values[1],
		patch: values[2],
	}, nil
}

func compareVersions(left, right releaseVersion) int {
	switch {
	case left.major < right.major:
		return -1
	case left.major > right.major:
		return 1
	case left.minor < right.minor:
		return -1
	case left.minor > right.minor:
		return 1
	case left.patch < right.patch:
		return -1
	case left.patch > right.patch:
		return 1
	default:
		return 0
	}
}
