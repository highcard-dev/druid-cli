package services

import (
	"testing"
	"unsafe"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func TestAddCommandOwnsBorrowedCommandName(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML(""))
	buffer := []byte("init_private")
	borrowed := unsafe.String(unsafe.SliceData(buffer), len(buffer))
	instruction := &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{Image: "busybox:1.37.0", Command: []string{"true"}}}}

	if err := session.AddCommand(borrowed, instruction); err != nil {
		t.Fatal(err)
	}
	copy(buffer, "dev_buildate")

	file := session.scrollService.GetFile()
	if _, ok := file.Commands["init_private"]; !ok {
		t.Fatalf("borrowed command key changed after request buffer reuse: %#v", file.Commands)
	}
	if _, ok := file.Commands["dev_buildate"]; ok {
		t.Fatalf("mutated command key was retained: %#v", file.Commands)
	}
}
