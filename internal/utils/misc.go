package utils

import (
	"errors"
	"fmt"
	"strings"
)

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

func InterfaceToStringSlice(data interface{}) ([]string, error) {

	instructionsRaw, ok := data.([]interface{})
	if !ok {
		return []string{}, errors.New("invalid instruction, expected array of strings")
	}

	// we have to manually []interface{} to []string :(
	instructions := make([]string, len(instructionsRaw))
	for i, v := range instructionsRaw {
		val, ok := v.(string)
		if !ok {
			return []string{}, errors.New("invalid instruction, cannot convert to string")
		}
		instructions[i] = val
	}
	return instructions, nil
}

func HumanizeBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes < KB:
		return fmt.Sprintf("%dB", bytes)
	case bytes < MB:
		return fmt.Sprintf("%.2fKB", float64(bytes)/KB)
	case bytes < GB:
		return fmt.Sprintf("%.2fMB", float64(bytes)/MB)
	case bytes < TB:
		return fmt.Sprintf("%.2fGB", float64(bytes)/GB)
	default:
		return fmt.Sprintf("%.2fTB", float64(bytes)/TB)
	}
}
