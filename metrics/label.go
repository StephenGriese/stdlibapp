package metrics

import (
	"strings"
)

func CanonicalLabel(s string) string {
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		if ('a' <= b && b <= 'z') ||
			('A' <= b && b <= 'Z') ||
			('0' <= b && b <= '9') ||
			b == '_' {
			result.WriteByte(b)
		} else if b == ' ' || b == '-' {
			result.WriteByte('_')
		}
	}

	return strings.ToLower(result.String())
}

func CanonicalLabels(labels []string) []string {
	formattedLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		formattedLabels = append(formattedLabels, CanonicalLabel(label))
	}

	return formattedLabels
}
