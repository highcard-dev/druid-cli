package workloadidentity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestKubernetesRegistryWatchesFreshCollectionRevision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: RegistryConfigMap, Namespace: "runtime", ResourceVersion: "1"}}
	client := fake.NewSimpleClientset(cm)
	client.PrependReactor("list", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.ConfigMapList{ListMeta: metav1.ListMeta{ResourceVersion: "1000"}, Items: []corev1.ConfigMap{*cm}}, nil
	})
	client.PrependWatchReactor("configmaps", func(action ktesting.Action) (bool, watch.Interface, error) {
		if action.(ktesting.WatchAction).GetWatchRestrictions().ResourceVersion != "1000" {
			return true, nil, apierrors.NewResourceExpired("object revision was compacted")
		}
		return true, watch.NewRaceFreeFake(), nil
	})
	registry, err := NewKubernetesRegistry(ctx, client, "runtime")
	if err != nil {
		t.Fatalf("idle ConfigMap failed to synchronize: %v", err)
	}
	if _, err := registry.Records(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestKubernetesRegistryRequiresInitialSyncAndRecoversAfterWatchLoss(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := fake.NewSimpleClientset()
	var unavailable atomic.Bool
	streams := make(chan *watch.RaceFreeFakeWatcher, 8)
	client.PrependWatchReactor("configmaps", func(ktesting.Action) (bool, watch.Interface, error) {
		if unavailable.Load() {
			return true, nil, errors.New("watch disconnected")
		}
		stream := watch.NewRaceFreeFake()
		streams <- stream
		return true, stream, nil
	})
	registry, err := NewKubernetesRegistry(ctx, client, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	stream := <-streams
	mount, err := registry.Ensure(ctx, Record{ExecutionID: "execution", DeploymentID: "deployment"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := client.CoreV1().Secrets("runtime").Get(ctx, mount.Secret, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	token := string(secret.Data["token"])
	cm, err := client.CoreV1().ConfigMaps("runtime").Get(ctx, RegistryConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stream.Modify(cm)
	wait := func(healthy bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			_, err := registry.Lookup(ctx, token)
			if (healthy && err == nil) || (!healthy && errors.Is(err, ErrUnavailable)) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("cache healthy=%v not reached: %v", healthy, err)
			}
			time.Sleep(time.Millisecond)
		}
	}
	wait(true)
	unavailable.Store(true)
	stream.Stop()
	wait(false)
	unavailable.Store(false)
	wait(true)

	blocked := fake.NewSimpleClientset()
	blocked.PrependWatchReactor("configmaps", func(ktesting.Action) (bool, watch.Interface, error) { return true, nil, errors.New("no watch") })
	short, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	if _, err := NewKubernetesRegistry(short, blocked, "runtime"); err == nil {
		t.Fatal("registry accepted requests before initial watch synchronization")
	}
}

func TestKubernetesRegistryPrunesOnlyStaleOrphanCredentials(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := fake.NewSimpleClientset()
	registry, err := NewKubernetesRegistry(ctx, client, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"old", "fresh"} {
		created := metav1.NewTime(time.Now().Add(-2 * time.Minute))
		if id == "fresh" {
			created = metav1.Now()
		}
		_, err := client.CoreV1().Secrets("runtime").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "druid-workload-" + id, CreationTimestamp: created, Labels: map[string]string{ExecutionLabel: id}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Prune(ctx, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Secrets("runtime").Get(ctx, "druid-workload-old", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("stale orphan retained")
	}
	if _, err := client.CoreV1().Secrets("runtime").Get(ctx, "druid-workload-fresh", metav1.GetOptions{}); err != nil {
		t.Fatal("fresh provisioner raced by pruning")
	}
}

func TestFileRegistryPreservesRevokesAndSeparatesCredentials(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	registry, err := NewFileRegistry(directory)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{ExecutionID: "first", DeploymentID: "deployment", Command: "automation", Permissions: []string{"queue:read"}}
	mount, err := registry.Ensure(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(mount.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(token), TokenPrefix) || len(token) != len(TokenPrefix)+43 {
		t.Fatal("invalid token format")
	}
	data, err := os.ReadFile(filepath.Join(directory, "registry.json"))
	if err != nil || strings.Contains(string(data), string(token)) {
		t.Fatal("raw credential in authorization registry")
	}
	reopened, err := NewFileRegistry(directory)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := reopened.Lookup(ctx, string(token))
	if err != nil || identity.DeploymentID != record.DeploymentID || !identity.Allows("queue:read") || identity.Allows("commands:run") {
		t.Fatal("identity was not preserved")
	}
	remount, err := reopened.Ensure(ctx, record)
	if err != nil || remount.Path != mount.Path {
		t.Fatal("reattachment replaced the credential")
	}
	if err := reopened.Revoke(ctx, record.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Lookup(ctx, string(token)); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked token accepted")
	}
	if _, err := reopened.Ensure(ctx, record); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("late provisioning resurrected a revoked execution")
	}
	if _, err := os.Stat(mount.Path); !os.IsNotExist(err) {
		t.Fatal("revoked credential file retained")
	}
	if Redact("echo "+string(token)) != "echo [REDACTED]" {
		t.Fatal("credential was not redacted")
	}
	if _, err := reopened.Ensure(ctx, Record{ExecutionID: "../escape"}); err == nil {
		t.Fatal("unsafe credential path accepted")
	}
}

func TestKubernetesRegistryConcurrentWritesCacheAndRevocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := fake.NewSimpleClientset()
	registry, err := NewKubernetesRegistry(ctx, client, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	client.PrependReactor("update", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		conflict := false
		once.Do(func() { conflict = true })
		if conflict {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, RegistryConfigMap, errors.New("fixture conflict"))
		}
		return false, nil, nil
	})
	var group sync.WaitGroup
	for _, execution := range []string{"one", "two", "three", "four"} {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			if _, err := registry.Ensure(ctx, Record{ExecutionID: id, DeploymentID: "deployment", Command: "agent", Permissions: []string{"logs:read"}}); err != nil {
				t.Errorf("ensure: %v", err)
			}
		}(execution)
	}
	group.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for {
		records, err := registry.Records(ctx)
		if err == nil && len(records) == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watch did not synchronize concurrent updates")
		}
		time.Sleep(time.Millisecond)
	}
	cm, err := client.CoreV1().ConfigMaps("runtime").Get(ctx, RegistryConfigMap, metav1.GetOptions{})
	if err != nil || len(cm.Data) != 4 {
		t.Fatal("registry updates were lost")
	}
	secret, err := client.CoreV1().Secrets("runtime").Get(ctx, "druid-workload-one", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	token := string(secret.Data["token"])
	for _, value := range cm.Data {
		if strings.Contains(value, token) {
			t.Fatal("raw credential exposed in ConfigMap")
		}
	}
	client.ClearActions()
	for index := 0; index < 10; index++ {
		if _, err := registry.Lookup(ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	if len(client.Actions()) != 0 {
		t.Fatal("authorization performed per-request API lookups")
	}
	if err := registry.Revoke(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := registry.Lookup(ctx, token)
		if errors.Is(err, ErrUnauthorized) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("revocation did not propagate")
		}
		time.Sleep(time.Millisecond)
	}
	registry.setCache(nil, false)
	if _, err := registry.Lookup(ctx, token); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unhealthy cache did not fail closed")
	}
	registry.setCache(map[string]string{}, true)
	if _, err := registry.Lookup(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("recovered cache accepted revoked credential")
	}
}
