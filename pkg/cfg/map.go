package cfg

import "reflect"

// MapAndRedact converts Config struct to a map and redacts unsafe fields.
func MapAndRedact(config any) map[string]any {
	result := map[string]any{}

	val := reflect.ValueOf(config)
	typ := reflect.TypeOf(config)

	if val.Kind() == reflect.Pointer {
		val = val.Elem()
		typ = typ.Elem()
	}

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		if field.PkgPath != "" {
			continue
		}

		yamlTag := field.Tag.Get("yaml")

		fieldName := field.Name
		if yamlTag != "" && yamlTag != "-" {
			fieldName = yamlTag
		}

		isSafe := field.Tag.Get("safe") == "true"

		if fieldVal.Kind() == reflect.Struct {
			result[fieldName] = MapAndRedact(fieldVal.Interface())
		} else if fieldVal.Kind() == reflect.Pointer && !fieldVal.IsNil() && fieldVal.Elem().Kind() == reflect.Struct {
			result[fieldName] = MapAndRedact(fieldVal.Elem().Interface())
		} else if isSafe {
			result[fieldName] = fieldVal.Interface()
		} else {
			result[fieldName] = "redacted"
		}
	}

	return result
}
