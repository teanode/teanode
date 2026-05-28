package ptrto

import "strings"

func TrimmedString(value string) *string {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return nil
	}
	return Value(trimmedValue)
}

// Trimmed is a generic version of TrimmedString for string-based enum types.
func Trimmed[StringType ~string](value string) *StringType {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return nil
	}
	result := StringType(trimmedValue)
	return &result
}

func TrimmedStrings(values []string) *[]string {
	if len(values) == 0 {
		return nil
	}
	trimmedValues := make([]string, 0, len(values))
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			continue
		}
		trimmedValues = append(trimmedValues, trimmedValue)
	}
	if len(trimmedValues) == 0 {
		return nil
	}
	return Value(trimmedValues)
}
