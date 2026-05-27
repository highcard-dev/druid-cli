package kubernetes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigWithDefaultsReadsKubeconfigEnv(t *testing.T) {
	t.Setenv("DRUID_K8S_KUBECONFIG", "/tmp/druid-kubeconfig")

	config := Config{}.WithDefaults()

	if config.Kubeconfig != "/tmp/druid-kubeconfig" {
		t.Fatalf("Kubeconfig = %s, want /tmp/druid-kubeconfig", config.Kubeconfig)
	}
}

func TestConfigWithDefaultsReadsRegistryPlainHTTPEnv(t *testing.T) {
	t.Setenv("DRUID_REGISTRY_PLAIN_HTTP", "true")

	config := Config{}.WithDefaults()

	if !config.RegistryPlainHTTP {
		t.Fatal("RegistryPlainHTTP = false, want true")
	}
}

func TestConfigHubbleEnabled(t *testing.T) {
	enabled := Config{HubbleRelayAddr: "hubble-relay.kube-system.svc.cluster.local:80"}
	if !enabled.HubbleEnabled() {
		t.Fatal("HubbleEnabled = false, want true")
	}

	for _, value := range []string{"disabled", "disable", "off", "false", "none", " DISABLED "} {
		if (Config{HubbleRelayAddr: value}).HubbleEnabled() {
			t.Fatalf("HubbleEnabled(%q) = true, want false", value)
		}
	}
}

func TestKubeconfigRESTConfigUsesCurrentContextNamespace(t *testing.T) {
	kubeconfig := writeKubeconfig(t, "from-context")

	_, namespace, source, err := kubeconfigRESTConfig(Config{Kubeconfig: kubeconfig})
	if err != nil {
		t.Fatal(err)
	}
	if namespace != "from-context" {
		t.Fatalf("namespace = %s, want from-context", namespace)
	}
	if source != kubeconfig {
		t.Fatalf("source = %s, want %s", source, kubeconfig)
	}
}

func TestKubeconfigRESTConfigExplicitNamespaceWins(t *testing.T) {
	kubeconfig := writeKubeconfig(t, "from-context")

	_, namespace, _, err := kubeconfigRESTConfig(Config{Kubeconfig: kubeconfig, Namespace: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	if namespace != "explicit" {
		t.Fatalf("namespace = %s, want explicit", namespace)
	}
}

func TestKubeconfigRESTConfigDefaultsNamespace(t *testing.T) {
	kubeconfig := writeKubeconfig(t, "")

	_, namespace, _, err := kubeconfigRESTConfig(Config{Kubeconfig: kubeconfig})
	if err != nil {
		t.Fatal(err)
	}
	if namespace != "default" {
		t.Fatalf("namespace = %s, want default", namespace)
	}
}

func TestKubeconfigRESTConfigUsesKUBECONFIGDefaultLoading(t *testing.T) {
	kubeconfig := writeKubeconfig(t, "from-env")
	t.Setenv("KUBECONFIG", kubeconfig)

	_, namespace, source, err := kubeconfigRESTConfig(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if namespace != "from-env" {
		t.Fatalf("namespace = %s, want from-env", namespace)
	}
	if source != "kubeconfig" {
		t.Fatalf("source = %s, want kubeconfig", source)
	}
}

func TestKubeconfigRESTConfigMissingExplicitPath(t *testing.T) {
	_, _, _, err := kubeconfigRESTConfig(Config{Kubeconfig: filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("error = nil, want missing kubeconfig error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v, want missing path", err)
	}
}

func writeKubeconfig(t *testing.T, namespace string) string {
	t.Helper()
	namespaceLine := ""
	if namespace != "" {
		namespaceLine = "    namespace: " + namespace + "\n"
	}
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
    insecure-skip-tls-verify: true
contexts:
- name: test
  context:
    cluster: test
    user: test
` + namespaceLine + `current-context: test
users:
- name: test
  user:
    token: test-token
`
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(kubeconfig), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
