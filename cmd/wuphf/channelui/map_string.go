package channelui

import "fmt"

// MapString reads key from m as a string. Missing / nil values yield
// "". String values are returned as-is. Other types are formatted via
// %v. Used to safely extract fields from JSON-decoded broker
// responses where the shape isn't statically known.
func MapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}
