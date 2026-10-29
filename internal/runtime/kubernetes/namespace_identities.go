package kubernetes

import (
	"context"
	"errors"
	"sync"

	"github.com/highcard-dev/daemon/internal/workloadidentity"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeclient "k8s.io/client-go/kubernetes"
)

// Runtime roots may select different namespaces. Keep each namespace's tokens
// and shared authorization ConfigMap beside its workloads, never in the daemon's
// default namespace. Authorization itself still uses synchronized local caches.
type namespaceIdentities struct {
	ctx              context.Context
	client           kubeclient.Interface
	defaultNamespace string
	mu               sync.Mutex
	registries       map[string]workloadidentity.Registry
	cancel           map[string]context.CancelFunc
	revoked          map[string]bool
}

func newNamespaceIdentities(ctx context.Context, client kubeclient.Interface, namespace string) (*namespaceIdentities, error) {
	r := &namespaceIdentities{ctx: ctx, client: client, defaultNamespace: namespace, registries: map[string]workloadidentity.Registry{}, cancel: map[string]context.CancelFunc{}, revoked: map[string]bool{}}
	if _, err := r.namespace(namespace); err != nil {
		return nil, err
	}
	if err := r.discover(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *namespaceIdentities) namespace(namespace string) (workloadidentity.Registry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if registry := r.registries[namespace]; registry != nil {
		return registry, nil
	}
	ctx, cancel := context.WithCancel(r.ctx)
	registry, err := workloadidentity.NewKubernetesRegistry(ctx, r.client, namespace)
	if err != nil {
		cancel()
		return nil, err
	}
	r.registries[namespace] = registry
	r.cancel[namespace] = cancel
	return registry, nil
}

func (r *namespaceIdentities) discover(ctx context.Context) error {
	maps, err := r.client.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{FieldSelector: "metadata.name=" + workloadidentity.RegistryConfigMap})
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, cm := range maps.Items {
		if cm.Name != workloadidentity.RegistryConfigMap {
			continue
		}
		if _, err := r.namespace(cm.Namespace); err != nil {
			return err
		}
		present[cm.Namespace] = true
	}
	// Deleted runtime namespaces must not leave failed watches blocking cleanup
	// in unrelated namespaces. A healthy newly-created watch is kept even if it
	// appeared after the list snapshot.
	r.mu.Lock()
	defer r.mu.Unlock()
	for namespace, registry := range r.registries {
		if namespace == r.defaultNamespace || present[namespace] {
			continue
		}
		if _, err := registry.Records(ctx); errors.Is(err, workloadidentity.ErrUnavailable) {
			r.cancel[namespace]()
			delete(r.cancel, namespace)
			delete(r.registries, namespace)
		}
	}
	return nil
}

func (r *namespaceIdentities) snapshot() []workloadidentity.Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	registries := make([]workloadidentity.Registry, 0, len(r.registries))
	for _, registry := range r.registries {
		registries = append(registries, registry)
	}
	return registries
}

func (r *namespaceIdentities) Ensure(ctx context.Context, record workloadidentity.Record) (workloadidentity.CredentialMount, error) {
	namespace := r.defaultNamespace
	if record.RuntimeRoot != "" {
		var err error
		namespace, _, err = parseRef(record.RuntimeRoot)
		if err != nil {
			return workloadidentity.CredentialMount{}, err
		}
	}
	registry, err := r.namespace(namespace)
	if err != nil {
		return workloadidentity.CredentialMount{}, err
	}
	r.mu.Lock()
	revoked := r.revoked[record.ExecutionID]
	r.mu.Unlock()
	if revoked {
		return workloadidentity.CredentialMount{}, workloadidentity.ErrUnauthorized
	}
	return registry.Ensure(ctx, record)
}

func (r *namespaceIdentities) Lookup(ctx context.Context, token string) (workloadidentity.Record, error) {
	var unavailable bool
	for _, registry := range r.snapshot() {
		record, err := registry.Lookup(ctx, token)
		if err == nil {
			r.mu.Lock()
			revoked := r.revoked[record.ExecutionID]
			r.mu.Unlock()
			if revoked {
				return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
			}
			return record, nil
		}
		unavailable = unavailable || errors.Is(err, workloadidentity.ErrUnavailable)
	}
	if unavailable {
		return workloadidentity.Record{}, workloadidentity.ErrUnavailable
	}
	return workloadidentity.Record{}, workloadidentity.ErrUnauthorized
}

func (r *namespaceIdentities) Records(ctx context.Context) ([]workloadidentity.Record, error) {
	if err := r.discover(ctx); err != nil {
		return nil, err
	}
	var records []workloadidentity.Record
	for _, registry := range r.snapshot() {
		values, err := registry.Records(ctx)
		if err != nil {
			return nil, err
		}
		records = append(records, values...)
	}
	return records, nil
}

func (r *namespaceIdentities) Revoke(ctx context.Context, execution string) error {
	r.mu.Lock()
	r.revoked[execution] = true
	r.mu.Unlock()
	for _, registry := range r.snapshot() {
		if err := registry.Revoke(ctx, execution); err != nil {
			return err
		}
	}
	return nil
}

func (r *namespaceIdentities) Prune(ctx context.Context, active map[string]bool) error {
	for _, registry := range r.snapshot() {
		if err := registry.Prune(ctx, active); err != nil {
			return err
		}
	}
	return nil
}
