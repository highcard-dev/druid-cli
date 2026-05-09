package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sclient "k8s.io/client-go/kubernetes"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

const (
	runtimeStateComponent = "runtime-state"

	configMapKeyID           = "id"
	configMapKeyOwnerID      = "owner_id"
	configMapKeyArtifact     = "artifact"
	configMapKeyScrollRoot   = "scroll_root"
	configMapKeyDataRoot     = "data_root"
	configMapKeyScrollName   = "scroll_name"
	configMapKeyScrollYAML   = "scroll_yaml"
	configMapKeyStatus       = "status"
	configMapKeyCreatedAt    = "created_at"
	configMapKeyUpdatedAt    = "updated_at"
	configMapKeyCommandsJSON = "commands_json"
)

type ConfigMapStateStore struct {
	client    k8sclient.Interface
	namespace string
}

func NewConfigMapStateStore(config Config) (*ConfigMapStateStore, error) {
	config = config.WithDefaults()
	restConfig, namespace, _, _, err := runtimeRESTConfig(config)
	if err != nil {
		return nil, err
	}
	client, err := k8sclient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	return NewConfigMapStateStoreWithClient(namespace, client), nil
}

func NewConfigMapStateStoreWithClient(namespace string, client k8sclient.Interface) *ConfigMapStateStore {
	if namespace == "" {
		namespace = "default"
	}
	return &ConfigMapStateStore{client: client, namespace: namespace}
}

func (s *ConfigMapStateStore) StateDir() string {
	return fmt.Sprintf("kubernetes:%s/configmaps", s.namespace)
}

func (s *ConfigMapStateStore) ScrollRoot(id string) string {
	return ref(s.namespace, dataPVCName(id))
}

func (s *ConfigMapStateStore) DataRoot(id string) string {
	return ref(s.namespace, dataPVCName(id))
}

func (s *ConfigMapStateStore) CreateScroll(scroll *domain.RuntimeScroll) error {
	now := time.Now().UTC()
	scroll.CreatedAt = now
	scroll.UpdatedAt = now
	if scroll.Status == "" {
		scroll.Status = domain.RuntimeScrollStatusCreated
	}
	if scroll.Commands == nil {
		scroll.Commands = map[string]domain.LockStatus{}
	}
	configMap, err := runtimeScrollConfigMap(s.namespace, scroll)
	if err != nil {
		return err
	}
	_, err = s.client.CoreV1().ConfigMaps(s.namespace).Create(context.Background(), configMap, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("%w: %s", coreservices.ErrScrollAlreadyExists, scroll.ID)
	}
	return err
}

func (s *ConfigMapStateStore) ListScrolls() ([]*domain.RuntimeScroll, error) {
	selector := labels.SelectorFromSet(labels.Set{
		labelManagedBy: "druid",
		labelComponent: runtimeStateComponent,
	})
	configMaps, err := s.client.CoreV1().ConfigMaps(s.namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	scrolls := make([]*domain.RuntimeScroll, 0, len(configMaps.Items))
	for i := range configMaps.Items {
		scroll, err := runtimeScrollFromConfigMap(&configMaps.Items[i])
		if err != nil {
			return nil, err
		}
		scrolls = append(scrolls, scroll)
	}
	sort.Slice(scrolls, func(i, j int) bool {
		return scrolls[i].ID < scrolls[j].ID
	})
	return scrolls, nil
}

func (s *ConfigMapStateStore) GetScroll(id string) (*domain.RuntimeScroll, error) {
	configMap, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(context.Background(), scrollConfigMapName(id), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, coreservices.ErrScrollNotFound
	}
	if err != nil {
		return nil, err
	}
	return runtimeScrollFromConfigMap(configMap)
}

func (s *ConfigMapStateStore) UpdateScroll(scroll *domain.RuntimeScroll) error {
	current, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(context.Background(), scrollConfigMapName(scroll.ID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return coreservices.ErrScrollNotFound
	}
	if err != nil {
		return err
	}
	scroll.UpdatedAt = time.Now().UTC()
	next, err := runtimeScrollConfigMap(s.namespace, scroll)
	if err != nil {
		return err
	}
	next.ResourceVersion = current.ResourceVersion
	_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(context.Background(), next, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		return coreservices.ErrScrollNotFound
	}
	return err
}

func (s *ConfigMapStateStore) DeleteScroll(id string) error {
	err := s.client.CoreV1().ConfigMaps(s.namespace).Delete(context.Background(), scrollConfigMapName(id), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return coreservices.ErrScrollNotFound
	}
	return err
}

func runtimeScrollConfigMap(namespace string, scroll *domain.RuntimeScroll) (*corev1.ConfigMap, error) {
	commands, err := json.Marshal(scroll.Commands)
	if err != nil {
		return nil, err
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scrollConfigMapName(scroll.ID),
			Namespace: namespace,
			Labels: map[string]string{
				labelManagedBy: "druid",
				labelComponent: runtimeStateComponent,
				labelScrollID:  dnsLabel(scroll.ID),
				"scroll-name":  dnsLabel(scroll.ScrollName),
			},
		},
		Data: map[string]string{
			configMapKeyID:           scroll.ID,
			configMapKeyOwnerID:      scroll.OwnerID,
			configMapKeyArtifact:     scroll.Artifact,
			configMapKeyScrollRoot:   scroll.ScrollRoot,
			configMapKeyDataRoot:     scroll.DataRoot,
			configMapKeyScrollName:   scroll.ScrollName,
			configMapKeyScrollYAML:   scroll.ScrollYAML,
			configMapKeyStatus:       string(scroll.Status),
			configMapKeyCreatedAt:    formatRuntimeTime(scroll.CreatedAt),
			configMapKeyUpdatedAt:    formatRuntimeTime(scroll.UpdatedAt),
			configMapKeyCommandsJSON: string(commands),
		},
	}, nil
}

func runtimeScrollFromConfigMap(configMap *corev1.ConfigMap) (*domain.RuntimeScroll, error) {
	data := configMap.Data
	commandsJSON := data[configMapKeyCommandsJSON]
	if commandsJSON == "" {
		commandsJSON = "{}"
	}
	commands := map[string]domain.LockStatus{}
	if err := json.Unmarshal([]byte(commandsJSON), &commands); err != nil {
		return nil, err
	}
	id := data[configMapKeyID]
	if id == "" {
		id = configMap.Labels[labelScrollID]
	}
	scroll := &domain.RuntimeScroll{
		ID:         id,
		OwnerID:    data[configMapKeyOwnerID],
		Artifact:   data[configMapKeyArtifact],
		ScrollRoot: data[configMapKeyScrollRoot],
		DataRoot:   data[configMapKeyDataRoot],
		ScrollName: data[configMapKeyScrollName],
		ScrollYAML: data[configMapKeyScrollYAML],
		Status:     domain.RuntimeScrollStatus(data[configMapKeyStatus]),
		CreatedAt:  parseRuntimeTime(data[configMapKeyCreatedAt]),
		UpdatedAt:  parseRuntimeTime(data[configMapKeyUpdatedAt]),
		Commands:   commands,
	}
	if scroll.Status == "" {
		scroll.Status = domain.RuntimeScrollStatusCreated
	}
	return scroll, nil
}

func scrollConfigMapName(id string) string {
	return dnsLabel("druid-scroll-" + id)
}

func formatRuntimeTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseRuntimeTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}
