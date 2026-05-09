package kubernetes

import (
	"fmt"
	"os"
	"strings"
)

const (
	defaultHubbleRelayAddr = "hubble-relay.kube-system.svc.cluster.local:80"
	defaultHelperImage     = "busybox:1.36"
)

type Config struct {
	Namespace         string
	StorageClass      string
	PullImage         string
	RegistrySecret    string
	RegistryPlainHTTP bool
	HubbleRelayAddr   string
	HelperImage       string
	Kubeconfig        string
}

func (c Config) WithDefaults() Config {
	if c.Namespace == "" {
		c.Namespace = os.Getenv("DRUID_K8S_NAMESPACE")
	}
	if c.StorageClass == "" {
		c.StorageClass = os.Getenv("DRUID_K8S_STORAGE_CLASS")
	}
	if c.PullImage == "" {
		c.PullImage = os.Getenv("DRUID_K8S_PULL_IMAGE")
	}
	if c.RegistrySecret == "" {
		c.RegistrySecret = os.Getenv("DRUID_K8S_REGISTRY_SECRET")
	}
	if !c.RegistryPlainHTTP {
		c.RegistryPlainHTTP = plainHTTPEnv("DRUID_REGISTRY_PLAIN_HTTP")
	}
	if c.Kubeconfig == "" {
		c.Kubeconfig = os.Getenv("DRUID_K8S_KUBECONFIG")
	}
	if c.HubbleRelayAddr == "" {
		c.HubbleRelayAddr = os.Getenv("DRUID_HUBBLE_RELAY_ADDR")
	}
	if c.HubbleRelayAddr == "" {
		c.HubbleRelayAddr = defaultHubbleRelayAddr
	}
	if c.HelperImage == "" {
		c.HelperImage = os.Getenv("DRUID_K8S_HELPER_IMAGE")
	}
	if c.HelperImage == "" {
		c.HelperImage = defaultHelperImage
	}
	return c
}

func plainHTTPEnv(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes"
}

func (c Config) ValidateForBackend() error {
	if c.Namespace == "" {
		return fmt.Errorf("kubernetes namespace is required")
	}
	return nil
}

func (c Config) ValidateForMaterialization() error {
	if c.PullImage == "" {
		return fmt.Errorf("kubernetes pull image is required for cluster materialization; set --k8s-pull-image or DRUID_K8S_PULL_IMAGE")
	}
	return nil
}

func namespaceFromServiceAccount() string {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
