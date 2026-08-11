package cfg

import "reflect"

// MapAndRedact converts Config struct to a map and redacts unsafe fields.
// Fields tagged safe:"true" are always included as-is.
// Unsafe fields are omitted when empty (empty string, nil pointer/interface) and shown as "redacted" when set.
// Safe struct fields recurse so their own field-level safe tags are respected.
func MapAndRedact(config any) map[string]any {
	result := map[string]any{}

	val := reflect.ValueOf(config)
	typ := reflect.TypeOf(config)

	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return result
		}
		val = val.Elem()
		typ = typ.Elem()
	}

	if val.Kind() != reflect.Struct {
		return result
	}

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		if field.PkgPath != "" {
			continue
		}

		fieldName := field.Name
		if yamlTag := field.Tag.Get("yaml"); yamlTag != "" && yamlTag != "-" {
			fieldName = yamlTag
		}

		isSafe := field.Tag.Get("safe") == "true"

		// Recurse into struct fields only when the parent is safe; otherwise redact the whole sub-struct.
		isStruct := fieldVal.Kind() == reflect.Struct
		isStructPtr := fieldVal.Kind() == reflect.Pointer && !fieldVal.IsNil() && fieldVal.Elem().Kind() == reflect.Struct

		if (isStruct || isStructPtr) && isSafe {
			inner := fieldVal.Interface()
			if isStructPtr {
				inner = fieldVal.Elem().Interface()
			}
			result[fieldName] = MapAndRedact(inner)
			continue
		}

		if isSafe {
			result[fieldName] = fieldVal.Interface()
			continue
		}

		// Unsafe: omit if empty (nothing to redact), otherwise redact.
		switch fieldVal.Kind() {
		case reflect.Ptr, reflect.Interface:
			if fieldVal.IsNil() {
				continue
			}
		case reflect.String:
			if fieldVal.String() == "" {
				continue
			}
		}
		result[fieldName] = "redacted"
	}

	return result
}
