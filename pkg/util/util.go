package util

func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

func SafeDeref[T any](v *T) T {
	if v != nil {
		return *v
	}

	var z T
	return z
}

func AsPtr[T any](v T) *T {
	return &v
}
