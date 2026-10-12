package imagepolicy

import "testing"

func TestShouldRefresh(t *testing.T) {
	for _, tc := range []struct {
		image string
		want  bool
	}{
		{"artifacts.druid.gg/druid-team/druid-dev:stable", true},
		{"druid:latest", true},
		{"alpine", true},
		{"alpine:3.20", true},
		{"registry.example:5000/team/image:release", true},
		{"registry.example:5000/team/image", true},
		{"druid@sha256:abc", false},
		{"druid:stable@sha256:abc", false},
		{"druid:local", false},
		{"registry.example:5000/druid:local", false},
	} {
		t.Run(tc.image, func(t *testing.T) {
			if got := ShouldRefresh(tc.image); got != tc.want {
				t.Fatalf("ShouldRefresh(%q) = %v, want %v", tc.image, got, tc.want)
			}
		})
	}
}
