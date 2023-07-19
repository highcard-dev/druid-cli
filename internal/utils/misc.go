package utils

import "strings"

func MapKeysToStringSlice(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func SplitArtifact(url string) (string, string) {
	parts := strings.Split(url, ":")
	if len(parts) != 2 {
		return "", ""
	}
	repo, tag := parts[0], parts[1]
	return repo, tag
}
