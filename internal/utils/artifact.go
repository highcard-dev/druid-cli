package utils

import "strings"

func SplitArtifact(url string) (string, string) {
	parts := strings.Split(url, ":")
	if len(parts) != 2 {
		return "", ""
	}
	repo, tag := parts[0], parts[1]
	return repo, tag
}

type ArtifactRefKind string

const (
	ArtifactRefKindTag    ArtifactRefKind = "tag"
	ArtifactRefKindDigest ArtifactRefKind = "digest"
)

// ParseArtifactRef parses an OCI-style artifact reference.
//
// Supported forms:
// - repo:tag
// - repo@sha256:<hex>
//
// It returns the repository (as passed through), the reference part (tag or digest),
// and the kind. If parsing fails, all return values are zero.
func ParseArtifactRef(s string) (repo string, ref string, kind ArtifactRefKind) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", ""
	}

	// Digest form: <repo>@sha256:<hex>
	if at := strings.LastIndex(s, "@"); at >= 0 {
		repoPart := s[:at]
		digestPart := s[at+1:]
		if repoPart == "" || digestPart == "" {
			return "", "", ""
		}
		// Minimal validation: require "sha256:" prefix and a non-empty hex payload.
		if !strings.HasPrefix(digestPart, "sha256:") {
			return "", "", ""
		}
		if len(digestPart) <= len("sha256:") {
			return "", "", ""
		}
		return repoPart, digestPart, ArtifactRefKindDigest
	}

	// Tag form: <repo>:<tag>
	// Use last ":" to avoid breaking on host:port registries.
	if colon := strings.LastIndex(s, ":"); colon >= 0 {
		repoPart := s[:colon]
		tagPart := s[colon+1:]
		if repoPart == "" || tagPart == "" {
			return "", "", ""
		}
		return repoPart, tagPart, ArtifactRefKindTag
	}

	return "", "", ""
}
