package web

import "time"

func optionalDeviceTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	return new(value.UTC().Format(time.RFC3339Nano))
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
