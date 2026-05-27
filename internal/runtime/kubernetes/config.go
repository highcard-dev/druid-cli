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
	UIS3Bucket        string
	UIS3PublicBaseURL string
	UIS3Region        string
	UIS3Endpoint      string
	UIS3Prefix        string
	UIS3Secret        string
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
	if c.UIS3Bucket == "" {
		c.UIS3Bucket = os.Getenv("DRUID_K8S_UI_S3_BUCKET")
	}
	if c.UIS3PublicBaseURL == "" {
		c.UIS3PublicBaseURL = os.Getenv("DRUID_K8S_UI_S3_PUBLIC_BASE_URL")
	}
	if c.UIS3Region == "" {
		c.UIS3Region = os.Getenv("DRUID_K8S_UI_S3_REGION")
	}
	if c.UIS3Endpoint == "" {
		c.UIS3Endpoint = os.Getenv("DRUID_K8S_UI_S3_ENDPOINT")
	}
	if c.UIS3Prefix == "" {
		c.UIS3Prefix = os.Getenv("DRUID_K8S_UI_S3_PREFIX")
	}
	if c.UIS3Secret == "" {
		c.UIS3Secret = os.Getenv("DRUID_K8S_UI_S3_CREDENTIALS_SECRET")
	}
	return c
}

func (c Config) HubbleEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(c.HubbleRelayAddr))
	return value != "disabled" && value != "disable" && value != "off" && value != "false" && value != "none"
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

func (c Config) ValidateForUIPublishing() error {
	if c.PullImage == "" {
		return fmt.Errorf("kubernetes pull image is required for UI publishing; set --k8s-pull-image or DRUID_K8S_PULL_IMAGE")
	}
	if c.UIS3Bucket == "" || c.UIS3PublicBaseURL == "" || c.UIS3Region == "" || c.UIS3Secret == "" {
		return fmt.Errorf("kubernetes UI publishing requires DRUID_K8S_UI_S3_BUCKET, DRUID_K8S_UI_S3_PUBLIC_BASE_URL, DRUID_K8S_UI_S3_REGION, and DRUID_K8S_UI_S3_CREDENTIALS_SECRET")
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
