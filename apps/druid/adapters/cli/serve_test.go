package cli

import (
	"reflect"
	"testing"
)

func TestBuildJWKSURLsNormalizesAndDeduplicates(t *testing.T) {
	got := buildJWKSURLs([]string{
		" https://old.example/jwks ",
		"",
		"https://api.druid.gg/auth/v2/jwks",
		"https://next.example/jwks",
		" https://old.example/jwks ",
		"https://api.druid.gg/auth/v2/jwks",
	})

	want := []string{
		"https://old.example/jwks",
		"https://api.druid.gg/auth/v2/jwks",
		"https://next.example/jwks",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildJWKSURLs() = %#v, want %#v", got, want)
	}
}

func TestBuildJWKSURLsEmptyWhenUnset(t *testing.T) {
	got := buildJWKSURLs([]string{"", "   "})
	if len(got) != 0 {
		t.Fatalf("buildJWKSURLs() = %#v, want empty slice", got)
	}
}
