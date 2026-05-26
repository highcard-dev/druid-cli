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
		"--worker-callback-listen=:8083",
		"--worker-daemon-url=http://druid-cli:8081",
		"--public-jwks-url=http://druid-cli:8082/.well-known/jwks.json",
		"name: management",
		"name: public",
		"name: worker",
		"DRUID_K8S_PULL_IMAGE",
		`resources: ["secrets"]`,
		`resources: ["pods/attach"]`,
		`verbs: ["create"]`,
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
		"--set", "auth.jwksUrl=https://auth.example.test/.well-known/jwks.json",
		"--set", "auth.publicJwksUrl=https://runtime.example.test/.well-known/jwks.json",
		"--set", "auth.existingSecret=druid-runtime-token",
		"--set", "runtime.namespaces.mode=all",
		"--set", "runtime.storageClass=local-path",
		"--set", "runtime.registryPlainHTTP=true",
		"--set", "runtime.pullImage=registry.local/druid-cli:e2e",
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
		"--auth-jwks-url=https://auth.example.test/.well-known/jwks.json",
		"--public-jwks-url=https://runtime.example.test/.well-known/jwks.json",
		"value: \"registry.local/druid-cli:e2e\"",
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
