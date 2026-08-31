package services

import (
	"strings"
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
	copy(buffer, "build_privte")

	file := session.scrollService.GetFile()
	if _, ok := file.Commands["init_private"]; !ok {
		t.Fatalf("borrowed command key changed after request buffer reuse: %#v", file.Commands)
	}
	if _, ok := file.Commands["build_privte"]; ok {
		t.Fatalf("mutated command key was retained: %#v", file.Commands)
	}
}

func TestAddCommandValidatesExpectedPortsAgainstFixedDeveloperPorts(t *testing.T) {
	session := newRuntimeSessionForTest(t, map[string]domain.LockStatus{}, cachedScrollYAML(""))
	session.runtimeScroll.ReservedPorts = fixedDeveloperPorts()

	webdav := &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{
		Image: "busybox:1.37.0", Command: []string{"true"}, ExpectedPorts: []domain.ExpectedPort{{Name: "webdav"}},
	}}}
	if err := session.AddCommand("druid_ui_dev", webdav); err != nil {
		t.Fatal(err)
	}

	unknown := &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{
		Image: "busybox:1.37.0", Command: []string{"true"}, ExpectedPorts: []domain.ExpectedPort{{Name: "not-declared"}},
	}}}
	err := session.AddCommand("invalid", unknown)
	if err == nil || !strings.Contains(err.Error(), "expected port not-declared is not defined") {
		t.Fatalf("AddCommand error = %v, want undeclared-port error", err)
	}
	if session.scrollService.GetFile().Commands["invalid"] != nil {
		t.Fatalf("invalid command was retained: %#v", session.scrollService.GetFile().Commands)
	}
}
