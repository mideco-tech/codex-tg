package daemon

import (
	"fmt"
	"strings"
)

func payloadString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return cleanPayloadString(typed)
	default:
		return cleanPayloadString(fmt.Sprintf("%v", typed))
	}
}

func cleanPayloadString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "<nil>" {
		return ""
	}
	return value
}

func payloadMapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	return payloadString(values[key])
}

func firstPayloadString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := payloadMapString(values, key); value != "" {
			return value
		}
	}
	return ""
}
