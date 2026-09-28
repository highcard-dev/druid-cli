package cli

import "testing"

func TestExplicitSourceDateMakesReleaseCreationDeterministic(t *testing.T) {
	for i := 0; i < 2; i++ {
		annotations := map[string]string{}
		if err := reproducibleCreatedAnnotation(annotations, "0"); err != nil {
			t.Fatal(err)
		}
		if annotations["org.opencontainers.image.created"] != "1970-01-01T00:00:00Z" {
			t.Fatal("unstable creation annotation")
		}
	}
	annotations := map[string]string{}
	if err := reproducibleCreatedAnnotation(annotations, ""); err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 0 {
		t.Fatal("ordinary pushes unexpectedly changed")
	}
	for _, invalid := range []string{"-1", "tomorrow", "253402300800"} {
		if err := reproducibleCreatedAnnotation(annotations, invalid); err == nil {
			t.Fatal("invalid source date accepted")
		}
	}
}
