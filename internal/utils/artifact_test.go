package utils

import "testing"

func TestParseArtifactRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		wantRepo string
		wantRef  string
		wantKind ArtifactRefKind
	}{
		{
			name:     "tag_simple",
			in:       "repo:tag",
			wantRepo: "repo",
			wantRef:  "tag",
			wantKind: ArtifactRefKindTag,
		},
		{
			name:     "tag_with_registry_port",
			in:       "localhost:5000/ns/repo:v1.2.3",
			wantRepo: "localhost:5000/ns/repo",
			wantRef:  "v1.2.3",
			wantKind: ArtifactRefKindTag,
		},
		{
			name:     "digest_basic",
			in:       "ghcr.io/acme/repo@sha256:deadbeef",
			wantRepo: "ghcr.io/acme/repo",
			wantRef:  "sha256:deadbeef",
			wantKind: ArtifactRefKindDigest,
		},
		{
			name:     "digest_trim_space",
			in:       "  ghcr.io/acme/repo@sha256:abc123  ",
			wantRepo: "ghcr.io/acme/repo",
			wantRef:  "sha256:abc123",
			wantKind: ArtifactRefKindDigest,
		},
		{
			name: "invalid_empty",
			in:   " ",
		},
		{
			name: "invalid_missing_tag",
			in:   "repo:",
		},
		{
			name: "invalid_missing_repo",
			in:   ":tag",
		},
		{
			name: "invalid_missing_digest",
			in:   "repo@sha256:",
		},
		{
			name: "invalid_digest_algo",
			in:   "repo@sha512:abc",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotRepo, gotRef, gotKind := ParseArtifactRef(tt.in)
			if gotRepo != tt.wantRepo || gotRef != tt.wantRef || gotKind != tt.wantKind {
				t.Fatalf("ParseArtifactRef(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.in, gotRepo, gotRef, gotKind,
					tt.wantRepo, tt.wantRef, tt.wantKind,
				)
			}
		})
	}
}

