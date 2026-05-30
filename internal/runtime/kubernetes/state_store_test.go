package kubernetes

import (
	"errors"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestConfigMapStateStoreRoundTripsRuntimeScroll(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	exitCode := 7
	scroll := &domain.RuntimeScroll{
		ID:         "container-lab",
		Artifact:   "registry.local/container-lab:1.0",
		Root:       ref("druid", "druid-container-lab-data"),
		ScrollName: "container-lab",
		ScrollYAML: "name: container-lab\n",
		Status:     domain.RuntimeScrollStatusCreated,
		Procedures: domain.ProcedureStatusMap{
			"verify": {
				"verify.0": {Status: domain.ScrollLockStatusError, ExitCode: &exitCode, LastStatusChange: 123},
			},
		},
	}

	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetScroll("container-lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Artifact != scroll.Artifact || got.Root != scroll.Root || got.ScrollYAML != scroll.ScrollYAML {
		t.Fatalf("stored scroll mismatch: %#v", got)
	}
	if got.Procedures["verify"]["verify.0"].Status != domain.ScrollLockStatusError {
		t.Fatalf("procedure status = %s, want error", got.Procedures["verify"]["verify.0"].Status)
	}
	if got.Procedures["verify"]["verify.0"].ExitCode == nil || *got.Procedures["verify"]["verify.0"].ExitCode != exitCode {
		t.Fatalf("exit code = %#v, want %d", got.Procedures["verify"]["verify.0"].ExitCode, exitCode)
	}

	got.Status = domain.RuntimeScrollStatusRunning
	got.Procedures["verify"]["verify.0"] = domain.LockStatus{Status: domain.ScrollLockStatusDone, LastStatusChange: 456}
	if err := store.UpdateScroll(got); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListScrolls()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != domain.RuntimeScrollStatusRunning || list[0].Procedures["verify"]["verify.0"].Status != domain.ScrollLockStatusDone {
		t.Fatalf("list = %#v, want updated scroll", list)
	}

	configMap, err := store.client.CoreV1().ConfigMaps("druid").Get(t.Context(), scrollConfigMapName("container-lab"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if configMap.Data[configMapKeyProceduresJSON] == "" {
		t.Fatal("procedures_json was not stored")
	}
	if _, ok := configMap.Data["commands_"+"json"]; ok {
		t.Fatal("legacy command status JSON was stored")
	}

	if err := store.DeleteScroll("container-lab"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScroll("container-lab"); !errors.Is(err, domain.ErrRuntimeScrollNotFound) {
		t.Fatalf("GetScroll after delete error = %v, want domain.ErrRuntimeScrollNotFound", err)
	}
}

func TestConfigMapStateStoreDuplicateCreateReturnsConflict(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	scroll := &domain.RuntimeScroll{
		ID:         "duplicate",
		Artifact:   "local",
		Root:       ref("druid", "druid-duplicate-data"),
		ScrollName: "duplicate",
		ScrollYAML: "name: duplicate\n",
	}

	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateScroll(scroll); !errors.Is(err, domain.ErrRuntimeScrollAlreadyExists) {
		t.Fatalf("CreateScroll duplicate error = %v, want domain.ErrRuntimeScrollAlreadyExists", err)
	}
}

func TestConfigMapStateStoreMissingScrollReturnsNotFound(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	if _, err := store.GetScroll("missing"); !errors.Is(err, domain.ErrRuntimeScrollNotFound) {
		t.Fatalf("GetScroll error = %v, want domain.ErrRuntimeScrollNotFound", err)
	}
	if err := store.DeleteScroll("missing"); !errors.Is(err, domain.ErrRuntimeScrollNotFound) {
		t.Fatalf("DeleteScroll error = %v, want domain.ErrRuntimeScrollNotFound", err)
	}
}

func TestConfigMapStateStoreDerivesKubernetesRoots(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	want := "k8s://druid/druid-container-lab-data"
	if got := store.Root("container-lab"); got != want {
		t.Fatalf("Root = %s, want %s", got, want)
	}
}
