package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestSelectConsoleContainerPrefersRunningThenNewest(t *testing.T) {
	selected := selectConsoleContainer([]container.Summary{
		{ID: "old-stopped", State: "exited", Created: 1},
		{ID: "running", State: "running", Created: 2},
		{ID: "new-stopped", State: "exited", Created: 3},
	})
	if selected.ID != "running" {
		t.Fatalf("selected = %s, want running", selected.ID)
	}

	selected = selectConsoleContainer([]container.Summary{
		{ID: "old", State: "exited", Created: 1},
		{ID: "new", State: "exited", Created: 2},
	})
	if selected.ID != "new" {
		t.Fatalf("selected = %s, want new", selected.ID)
	}
}
