package kubernetes

import "testing"

func TestProcedureResourceNamePrefixesNumericRuntimeID(t *testing.T) {
	root := ref("games", dataPVCName("0636a354-b3f4-4471-8749-3890e675a01c"))

	if got, want := procedureResourceName(root, "start", 1), "d-0636a354-b3f4-4471-8749-3890e675a01c-start-1"; got != want {
		t.Fatalf("procedureResourceName = %s, want %s", got, want)
	}
	if got, want := serviceName(root, "start", "main"), "d-0636a354-b3f4-4471-8749-3890e675a01c-start-main"; got != want {
		t.Fatalf("serviceName = %s, want %s", got, want)
	}
}

func TestProcedureResourceNameKeepsReadableAlphaRuntimeID(t *testing.T) {
	root := ref("games", dataPVCName("deployment-123"))

	if got, want := procedureResourceName(root, "start", 0), "deployment-123-start-0"; got != want {
		t.Fatalf("procedureResourceName = %s, want %s", got, want)
	}
}
