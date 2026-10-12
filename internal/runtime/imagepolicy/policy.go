package imagepolicy

import "strings"

// ShouldRefresh checks registry tags on each container start. Only digests are
// immutable; :local images are explicitly built and imported for development.
func ShouldRefresh(image string) bool {
	return !strings.Contains(image, "@") && !strings.HasSuffix(image, ":local")
}
