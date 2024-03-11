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

func ParseProcessAndCommand(processAndCommand string) (string, string) {
	parts := strings.Split(processAndCommand, ".")

	if len(parts) == 1 {
		return "main", processAndCommand
	}

	if len(parts) != 2 {
		return "", ""
	}
	process, command := parts[0], parts[1]
	return process, command
}
