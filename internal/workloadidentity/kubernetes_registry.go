package workloadidentity

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const RegistryConfigMap = "druid-workload-identities"

type KubernetesRegistry struct {
	revoked   map[string]bool
	writeMu   sync.Mutex
	client    kubernetes.Interface
	namespace string
	mu        sync.RWMutex
	records   map[string]Record
	healthy   bool
}

func NewKubernetesRegistry(ctx context.Context, client kubernetes.Interface, namespace string) (*KubernetesRegistry, error) {
	r := &KubernetesRegistry{client: client, namespace: namespace, revoked: map[string]bool{}}
	_, err := client.CoreV1().ConfigMaps(namespace).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: RegistryConfigMap}, Data: map[string]string{}}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, err
	}
	ready := make(chan struct{})
	go r.watch(ctx, ready)
	select {
	case <-ready:
		return r, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		return nil, ErrUnavailable
	}
}

func decodeRecords(data map[string]string) (map[string]Record, error) {
	records := make(map[string]Record, len(data))
	for key, value := range data {
		var record Record
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			return nil, err
		}
		records[key] = record
	}
	return records, nil
}

func (r *KubernetesRegistry) setCache(data map[string]string, healthy bool) {
	records, err := decodeRecords(data)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = records
	r.healthy = healthy && err == nil
}

// Use the collection revision: an idle object's revision may already have been
// compacted. Every disconnect fails closed until the next list/watch sync.
func (r *KubernetesRegistry) watch(ctx context.Context, ready chan struct{}) {
	var initial sync.Once
	for ctx.Err() == nil {
		maps, err := r.client.CoreV1().ConfigMaps(r.namespace).List(ctx, metav1.ListOptions{FieldSelector: "metadata.name=" + RegistryConfigMap})
		if err == nil && len(maps.Items) == 1 {
			var stream watch.Interface
			stream, err = r.client.CoreV1().ConfigMaps(r.namespace).Watch(ctx, metav1.ListOptions{FieldSelector: "metadata.name=" + RegistryConfigMap, ResourceVersion: maps.ResourceVersion, TimeoutSeconds: ptrInt64(300)})
			if err == nil {
				r.setCache(maps.Items[0].Data, true)
				initial.Do(func() { close(ready) })
			watching:
				for {
					select {
					case <-ctx.Done():
						break watching
					case event, ok := <-stream.ResultChan():
						if !ok || event.Type == watch.Error || event.Type == watch.Deleted {
							break watching
						}
						if updated, ok := event.Object.(*corev1.ConfigMap); ok {
							r.setCache(updated.Data, true)
						}
					}
				}
				stream.Stop()
			}
		}
		r.setCache(nil, false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	r.setCache(nil, false)
}

func ptrInt64(value int64) *int64 { return &value }

func (r *KubernetesRegistry) mutate(ctx context.Context, change func(map[string]string) error) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, RegistryConfigMap, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		if err := change(cm.Data); err != nil {
			return err
		}
		_, err = r.client.CoreV1().ConfigMaps(r.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

func (r *KubernetesRegistry) Ensure(ctx context.Context, record Record) (CredentialMount, error) {
	name := "druid-workload-" + record.ExecutionID
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, name, metav1.GetOptions{})
	created := false
	if apierrors.IsNotFound(err) {
		if record.Recover {
			return CredentialMount{}, ErrUnauthorized
		}
		token, tokenErr := NewToken()
		if tokenErr != nil {
			return CredentialMount{}, tokenErr
		}
		secret, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{ExecutionLabel: record.ExecutionID}},
			Data:       map[string][]byte{"token": []byte(token)},
		}, metav1.CreateOptions{})
		created = err == nil
		if apierrors.IsAlreadyExists(err) {
			secret, err = r.client.CoreV1().Secrets(r.namespace).Get(ctx, name, metav1.GetOptions{})
		}
	}
	if err != nil {
		return CredentialMount{}, err
	}
	if len(secret.Data["token"]) == 0 {
		return CredentialMount{}, fmt.Errorf("execution credential is missing")
	}
	hash := Hash(string(secret.Data["token"]))
	err = r.mutate(ctx, func(data map[string]string) error {
		if r.revoked[record.ExecutionID] {
			return ErrUnauthorized
		}
		if existing, ok := data[hash]; ok {
			var old Record
			if err := json.Unmarshal([]byte(existing), &old); err != nil {
				return err
			}
			if old.ExecutionID != record.ExecutionID || old.DeploymentID != record.DeploymentID || old.Command != record.Command {
				return ErrUnauthorized
			}
			return nil // Persisted grants, not a new request, remain authoritative.
		}
		if !created {
			return ErrUnauthorized
		} // Never resurrect a revoked credential.
		value, err := json.Marshal(record)
		if err != nil {
			return err
		}
		data[hash] = string(value)
		return nil
	})
	if err != nil {
		if created {
			_ = r.client.CoreV1().Secrets(r.namespace).Delete(ctx, name, metav1.DeleteOptions{})
		}
		return CredentialMount{}, err
	}
	return CredentialMount{Secret: name}, nil
}

func (r *KubernetesRegistry) Lookup(_ context.Context, token string) (Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.healthy {
		return Record{}, ErrUnavailable
	}
	record, ok := r.records[Hash(token)]
	if !ok {
		return Record{}, ErrUnauthorized
	}
	return record, nil
}

func (r *KubernetesRegistry) Records(_ context.Context) ([]Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.healthy {
		return nil, ErrUnavailable
	}
	records := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		records = append(records, record)
	}
	return records, nil
}

func (r *KubernetesRegistry) Revoke(ctx context.Context, execution string) error {
	if err := r.mutate(ctx, func(data map[string]string) error {
		r.revoked[execution] = true
		for key, value := range data {
			var record Record
			if err := json.Unmarshal([]byte(value), &record); err != nil {
				return err
			}
			if record.ExecutionID == execution {
				delete(data, key)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, "druid-workload-"+execution, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *KubernetesRegistry) Prune(ctx context.Context, active map[string]bool) error {
	records, err := r.Records(ctx)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, record := range records {
		known[record.ExecutionID] = true
	}
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{LabelSelector: ExecutionLabel})
	if err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		execution := secret.Labels[ExecutionLabel]
		// Secret creation precedes registry publication. Never race a fresh
		// provisioner (including another daemon) or its watch propagation.
		if time.Since(secret.CreationTimestamp.Time) < time.Minute {
			continue
		}
		if known[execution] || active[execution] || secret.Name != "druid-workload-"+execution {
			continue
		}
		if err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
