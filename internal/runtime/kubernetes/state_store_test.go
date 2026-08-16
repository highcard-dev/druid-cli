package kubernetes

import (
	"errors"
	"testing"
	"time"

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
		ReservedPorts: []domain.Port{
			{Name: "vscode", Port: 3333, Protocol: "http"},
		},
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
	if len(got.ReservedPorts) != 1 || got.ReservedPorts[0].Name != "vscode" {
		t.Fatalf("reserved ports = %#v, want vscode", got.ReservedPorts)
	}
	got.ReservedPorts = []domain.Port{}
	if err := store.UpdateScroll(got); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetScroll("container-lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReservedPorts == nil || len(got.ReservedPorts) != 0 {
		t.Fatalf("reserved ports = %#v, want explicit empty list", got.ReservedPorts)
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
	if configMap.Data[configMapKeyReservedPorts] == "" {
		t.Fatal("reserved_ports_json was not stored")
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

func TestConfigMapStateStoreReadsLegacyReservedPortFields(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	scroll := &domain.RuntimeScroll{
		ID: "legacy-reservation", Artifact: "local", Root: ref("druid", "druid-legacy-reservation-data"),
		ScrollName: "legacy-reservation", ScrollYAML: "name: legacy-reservation\n",
	}
	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	configMap, err := store.client.CoreV1().ConfigMaps("druid").Get(t.Context(), scrollConfigMapName(scroll.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	configMap.Data[configMapKeyReservedPorts] = `[{"name":"ssh","port":2222,"protocol":"tcp","command":"ssh","procedure":"ssh"}]`
	if _, err := store.client.CoreV1().ConfigMaps("druid").Update(t.Context(), configMap, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetScroll(scroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ReservedPorts) != 1 || got.ReservedPorts[0] != (domain.Port{Name: "ssh", Port: 2222, Protocol: "tcp"}) {
		t.Fatalf("reserved ports = %#v", got.ReservedPorts)
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

func TestConfigMapStateStorePreservesUIPackageScopesFromStaleUpdate(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	scroll := &domain.RuntimeScroll{
		ID:         "ui-packages",
		Artifact:   "local",
		Root:       ref("druid", "druid-ui-packages-data"),
		ScrollName: "ui-packages",
		ScrollYAML: "name: ui-packages\n",
	}
	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	stale, err := store.GetScroll(scroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateUpdate, err := store.GetScroll(scroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateUpdate.UIPackages = domain.RuntimeUIPackages{
		domain.RuntimeUIPackageScopePrivate: {Path: "private/dist/app.wasm", SHA256: "private"},
	}
	if err := store.UpdateScroll(privateUpdate); err != nil {
		t.Fatal(err)
	}

	stale.UIPackages = domain.RuntimeUIPackages{
		domain.RuntimeUIPackageScopePublic: {Path: "public/dist/app.wasm", SHA256: "public"},
	}
	if err := store.UpdateScroll(stale); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetScroll(scroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UIPackages[domain.RuntimeUIPackageScopePrivate].SHA256 != "private" {
		t.Fatalf("private package = %#v", got.UIPackages[domain.RuntimeUIPackageScopePrivate])
	}
	if got.UIPackages[domain.RuntimeUIPackageScopePublic].SHA256 != "public" {
		t.Fatalf("public package = %#v", got.UIPackages[domain.RuntimeUIPackageScopePublic])
	}
}

func TestConfigMapStateStorePreservesNewerUIPackageFromStaleUpdate(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	older := time.Now().UTC().Add(-time.Minute)
	newer := time.Now().UTC()
	scroll := &domain.RuntimeScroll{
		ID: "ui-package-race", Artifact: "local", Root: ref("druid", "ui-package-race-data"),
		ScrollName: "ui-package-race", ScrollYAML: "name: ui-package-race\n",
		UIPackages: domain.RuntimeUIPackages{
			domain.RuntimeUIPackageScopePrivate: {SHA256: "old", UpdatedAt: older},
		},
	}
	if err := store.CreateScroll(scroll); err != nil {
		t.Fatal(err)
	}

	stale, err := store.GetScroll(scroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := store.GetScroll(scroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh.UIPackages[domain.RuntimeUIPackageScopePrivate] = domain.RuntimeUIPackage{SHA256: "new", UpdatedAt: newer}
	if err := store.UpdateScroll(fresh); err != nil {
		t.Fatal(err)
	}

	stale.ScrollYAML = "name: stale-command-cleanup\n"
	if err := store.UpdateScroll(stale); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetScroll(scroll.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pkg := got.UIPackages[domain.RuntimeUIPackageScopePrivate]; pkg.SHA256 != "new" || !pkg.UpdatedAt.Equal(newer) {
		t.Fatalf("private package = %#v, want newer concurrent publish", pkg)
	}
}

func TestConfigMapStateStoreDerivesKubernetesRoots(t *testing.T) {
	store := NewConfigMapStateStoreWithClient("druid", fake.NewSimpleClientset())
	want := "k8s://druid/druid-container-lab-data"
	if got := store.Root("container-lab"); got != want {
		t.Fatalf("Root = %s, want %s", got, want)
	}
}
