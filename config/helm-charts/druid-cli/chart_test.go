package druidcli_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestChartRendersDefaultAndCustomValues(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}

	defaultManifest := helmTemplate(t)
	for _, want := range []string{
		"kind: Deployment",
		"kind: Service",
		"kind: Role",
		"kind: RoleBinding",
		"--runtime=kubernetes",
		"--listen=:8081",
		"--public-listen=:8082",
		"name: management",
		"name: public",
		"DRUID_K8S_PULL_IMAGE",
		"hubble-relay.kube-system.svc.cluster.local:80",
	} {
		if !strings.Contains(defaultManifest, want) {
			t.Fatalf("default manifest does not contain %q", want)
		}
	}
	if strings.Contains(defaultManifest, `resources: ["*"]`) || strings.Contains(defaultManifest, `verbs: ["*"]`) {
		t.Fatal("chart rendered wildcard RBAC")
	}

	customManifest := helmTemplate(t,
		"--set", "auth.enabled=true",
		"--set", "auth.existingSecret=druid-runtime-token",
		"--set", "runtime.namespaces.mode=all",
		"--set", "runtime.storageClass=local-path",
		"--set", "runtime.registryPlainHTTP=true",
		"--set", "runtime.pullImage=registry.local/druid-client:e2e",
		"--set", "runtime.helperImage=busybox:1.36",
		"--set", "runtime.kubeconfigSecret.name=druid-kubeconfig",
		"--set", "hubble.relayAddr=hubble.example:80",
		"--set", "networkPolicy.enabled=true",
		"--set", "ingress.enabled=true",
		"--set", "ingress.hosts[0].host=runtime.example.test",
		"--set", "ingress.hosts[0].paths[0].path=/",
	)
	for _, want := range []string{
		"kind: ClusterRole",
		"kind: ClusterRoleBinding",
		"name: DRUID_INTERNAL_TOKEN",
		"name: \"druid-runtime-token\"",
		"value: \"registry.local/druid-client:e2e\"",
		"value: \"busybox:1.36\"",
		"value: \"true\"",
		"value: /etc/druid/kubeconfig",
		"hubble.example:80",
		"kind: NetworkPolicy",
		"kind: Ingress",
		"runtime.example.test",
		"name: public",
		"nginx.ingress.kubernetes.io/enable-cors",
		"storageClassName: \"local-path\"",
	} {
		if !strings.Contains(customManifest, want) {
			t.Fatalf("custom manifest does not contain %q", want)
		}
	}
}

func helmTemplate(t *testing.T, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"template", "druid-cli", "."}, args...)
	cmd := exec.Command("helm", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, string(out))
	}
	return string(out)
}
