package sessions

import (
	"fmt"
	"strings"
	"unicode"
)

// NormalizeGroup gives every client the same stable hierarchy and blank-to-clear
// behavior. Group names are labels, never filesystem paths.
func NormalizeGroup(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 240 {
		return "", fmt.Errorf("group path must be at most 240 bytes")
	}
	parts := strings.Split(value, "/")
	if len(parts) > 8 {
		return "", fmt.Errorf("group hierarchy is limited to 8 levels")
	}
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("use nonempty group names separated by / ")
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return "", fmt.Errorf("group names cannot contain control characters")
			}
		}
		parts[i] = part
	}
	return strings.Join(parts, "/"), nil
}
