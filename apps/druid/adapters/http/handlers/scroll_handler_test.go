package handlers

import "testing"

func TestRuntimeRootsAcceptsOmittedOrEqualLegacyRoots(t *testing.T) {
	scrollRoot, dataRoot, err := runtimeRoots(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if scrollRoot != "" || dataRoot != "" {
		t.Fatalf("roots = %q/%q, want empty", scrollRoot, dataRoot)
	}

	root := "/runtime/root"
	scrollRoot, dataRoot, err = runtimeRoots(&root, &root)
	if err != nil {
		t.Fatal(err)
	}
	if scrollRoot != root || dataRoot != root {
		t.Fatalf("roots = %q/%q, want %q", scrollRoot, dataRoot, root)
	}
}

func TestRuntimeRootsRejectsSplitLegacyRoots(t *testing.T) {
	scrollRoot := "/runtime/spec"
	dataRoot := "/runtime/data"
	if _, _, err := runtimeRoots(&scrollRoot, &dataRoot); err == nil {
		t.Fatal("expected split roots to fail")
	}
}
