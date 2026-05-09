package kubernetes

import (
	"errors"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestConfigMapStateStoreRoundTripsRuntimeScroll(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	exitCode := 7
	scroll := &domain.RuntimeScroll{
		ID:         "container-lab",
		Artifact:   "registry.local/container-lab:1.0",
		ScrollRoot: ref("druid", "druid-container-lab-data"),
		DataRoot:   ref("druid", "druid-container-lab-data"),
		ScrollName: "container-lab",
		ScrollYAML: "name: container-lab\n",
		Status:     domain.RuntimeScrollStatusCreated,
		Commands: map[string]domain.LockStatus{
			"verify": {Status: domain.ScrollLockStatusError, ExitCode: &exitCode, LastStatusChange: 123},
		},
	}

	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetScroll("container-lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Artifact != scroll.Artifact || got.ScrollRoot != scroll.ScrollRoot || got.ScrollYAML != scroll.ScrollYAML {
		t.Fatalf("stored scroll mismatch: %#v", got)
	}
	if got.Commands["verify"].Status != domain.ScrollLockStatusError {
		t.Fatalf("command status = %s, want error", got.Commands["verify"].Status)
	}
	if got.Commands["verify"].ExitCode == nil || *got.Commands["verify"].ExitCode != exitCode {
		t.Fatalf("exit code = %#v, want %d", got.Commands["verify"].ExitCode, exitCode)
	}

	got.Status = domain.RuntimeScrollStatusRunning
	got.Commands["verify"] = domain.LockStatus{Status: domain.ScrollLockStatusDone, LastStatusChange: 456}
	if err := store.UpdateScroll(got); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListScrolls()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != domain.RuntimeScrollStatusRunning || list[0].Commands["verify"].Status != domain.ScrollLockStatusDone {
		t.Fatalf("list = %#v, want updated scroll", list)
	}

	configMap, err := store.client.CoreV1().ConfigMaps("druid").Get(t.Context(), scrollConfigMapName("container-lab"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if configMap.Data[configMapKeyCommandsJSON] == "" {
		t.Fatal("commands_json was not stored")
	}

	if err := store.DeleteScroll("container-lab"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScroll("container-lab"); !errors.Is(err, coreservices.ErrScrollNotFound) {
		t.Fatalf("GetScroll after delete error = %v, want ErrScrollNotFound", err)
	}
}

func TestConfigMapStateStoreDuplicateCreateReturnsConflict(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	scroll := &domain.RuntimeScroll{
		ID:         "duplicate",
		Artifact:   "local",
		ScrollRoot: ref("druid", "druid-duplicate-data"),
		DataRoot:   ref("druid", "druid-duplicate-data"),
		ScrollName: "duplicate",
		ScrollYAML: "name: duplicate\n",
	}

	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateScroll(scroll); !errors.Is(err, coreservices.ErrScrollAlreadyExists) {
		t.Fatalf("CreateScroll duplicate error = %v, want ErrScrollAlreadyExists", err)
	}
}

func TestConfigMapStateStoreMissingScrollReturnsNotFound(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	if _, err := store.GetScroll("missing"); !errors.Is(err, coreservices.ErrScrollNotFound) {
		t.Fatalf("GetScroll error = %v, want ErrScrollNotFound", err)
	}
	if err := store.DeleteScroll("missing"); !errors.Is(err, coreservices.ErrScrollNotFound) {
		t.Fatalf("DeleteScroll error = %v, want ErrScrollNotFound", err)
	}
}

func TestConfigMapStateStoreDerivesKubernetesRoots(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	want := "k8s://druid/druid-container-lab-data"
	if got := store.ScrollRoot("container-lab"); got != want {
		t.Fatalf("ScrollRoot = %s, want %s", got, want)
	}
	if got := store.DataRoot("container-lab"); got != want {
		t.Fatalf("DataRoot = %s, want %s", got, want)
	}
}
