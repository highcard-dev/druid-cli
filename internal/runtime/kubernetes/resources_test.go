package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/highcard-dev/daemon/internal/core/domain"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

type fakeHubble struct {
	hasFlow bool
	err     error
}

func (f fakeHubble) HasFlow(context.Context, TrafficQuery) (bool, error) {
	return f.hasFlow, f.err
}

func TestProcedureJobSpecBuildsDeterministicMountsAndLabels(t *testing.T) {
	procedure := &domain.Procedure{
		Image:      "alpine:3.20",
		Command:    []string{"sh", "-c", "echo ok"},
		WorkingDir: "/work",
		Env: map[string]string{
			"B": "two",
			"A": "one",
		},
		Mounts: []domain.Mount{{Path: "/work", SubPath: "cache"}},
	}

	job, err := procedureJobSpec("druid", ref("druid", "druid-static-web-data"), "start", procedure, "registry-secret")
	if err != nil {
		t.Fatal(err)
	}

	if job.Namespace != "druid" {
		t.Fatalf("namespace = %s, want druid", job.Namespace)
	}
	if job.Labels[labelManagedBy] != "druid" || job.Labels[labelProcedure] != "start" {
		t.Fatalf("labels = %#v", job.Labels)
	}
	pod := job.Spec.Template.Spec
	if len(pod.ImagePullSecrets) != 1 || pod.ImagePullSecrets[0].Name != "registry-secret" {
		t.Fatalf("image pull secrets = %#v", pod.ImagePullSecrets)
	}
	container := pod.Containers[0]
	if container.Image != "alpine:3.20" {
		t.Fatalf("image = %s", container.Image)
	}
	if got := container.VolumeMounts[0].SubPath; got != "data/cache" {
		t.Fatalf("subPath = %s, want data/cache", got)
	}
	if container.Env[0].Name != "A" || container.Env[1].Name != "B" {
		t.Fatalf("env order = %#v", container.Env)
	}
}

func TestProcedureStatefulSetSpecBuildsPersistentWorkload(t *testing.T) {
	procedure := &domain.Procedure{
		Image:         "nginx:1.27",
		Command:       []string{"nginx", "-g", "daemon off;"},
		ExpectedPorts: []domain.ExpectedPort{{Name: "http"}},
		Mounts:        []domain.Mount{{Path: "/usr/share/nginx/html", SubPath: "site", ReadOnly: true}},
	}

	statefulSet, err := procedureStatefulSetSpec("druid", ref("druid", "druid-static-web-data"), "start", procedure, "registry-secret")
	if err != nil {
		t.Fatal(err)
	}

	if statefulSet.Namespace != "druid" {
		t.Fatalf("namespace = %s, want druid", statefulSet.Namespace)
	}
	if statefulSet.Name != statefulSetName(ref("druid", "druid-static-web-data"), "start") {
		t.Fatalf("name = %s", statefulSet.Name)
	}
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 {
		t.Fatalf("replicas = %#v, want 1", statefulSet.Spec.Replicas)
	}
	if statefulSet.Spec.Selector.MatchLabels[labelProcedure] != "start" {
		t.Fatalf("selector = %#v", statefulSet.Spec.Selector.MatchLabels)
	}
	pod := statefulSet.Spec.Template.Spec
	if len(pod.ImagePullSecrets) != 1 || pod.ImagePullSecrets[0].Name != "registry-secret" {
		t.Fatalf("image pull secrets = %#v", pod.ImagePullSecrets)
	}
	container := pod.Containers[0]
	if container.Image != "nginx:1.27" {
		t.Fatalf("image = %s", container.Image)
	}
	if got := container.VolumeMounts[0].SubPath; got != "data/site" {
		t.Fatalf("subPath = %s, want data/site", got)
	}
	if !container.VolumeMounts[0].ReadOnly {
		t.Fatal("mount should be read-only")
	}
}

func TestPullJobSpecPropagatesPlainHTTPRegistryEnv(t *testing.T) {
	job := pullJobSpec("druid", "pull", "scroll-pvc", "druid-client:test", "registry:5000/lab:1.0", "", true)

	env := job.Spec.Template.Spec.Containers[0].Env
	if len(env) != 1 || env[0].Name != "DRUID_REGISTRY_PLAIN_HTTP" || env[0].Value != "true" {
		t.Fatalf("env = %#v", env)
	}
}

func TestExpectedPortsUsesHubbleFlowPresence(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{hasFlow: true})
	dataRoot := ref("druid", "druid-static-web-data")
	procedureName := "start"
	service, err := serviceSpec("druid", dataRoot, procedureName, "http", domain.Port{Name: "http", Port: 80, Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Services("druid").Create(context.Background(), service, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	ready := true
	if _, err := client.DiscoveryV1().EndpointSlices("druid").Create(context.Background(), &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "http",
			Namespace: "druid",
			Labels:    map[string]string{"kubernetes.io/service-name": service.Name},
		},
		Endpoints: []discoveryv1.Endpoint{{Conditions: discoveryv1.EndpointConditions{Ready: &ready}}},
		Ports:     []discoveryv1.EndpointPort{{Name: &service.Spec.Ports[0].Name}},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	statuses, err := backend.ExpectedPorts(dataRoot, map[string]*domain.CommandInstructionSet{
		"start": {Procedures: []*domain.Procedure{{
			Id:            &procedureName,
			ExpectedPorts: []domain.ExpectedPort{{Name: "http", KeepAliveTraffic: "1b/5m"}},
		}}},
	}, []domain.Port{{Name: "http", Port: 80, Protocol: "tcp"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if !status.Bound || !status.Traffic || status.TrafficOK == nil || !*status.TrafficOK {
		t.Fatalf("status = %#v", status)
	}
	if status.Source != "hubble-relay" {
		t.Fatalf("source = %s, want hubble-relay", status.Source)
	}
	if status.RXBytes != nil || status.TXBytes != nil || status.TrafficBytes != nil {
		t.Fatalf("byte counters should be nil for Kubernetes Hubble status: %#v", status)
	}
}

func TestExpectedPortsDegradesWhenHubbleUnavailable(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{err: errors.New("relay unavailable")})
	dataRoot := ref("druid", "druid-static-web-data")
	service, err := serviceSpec("druid", dataRoot, "start", "http", domain.Port{Name: "http", Port: 80, Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Services("druid").Create(context.Background(), service, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	statuses, err := backend.ExpectedPorts(dataRoot, map[string]*domain.CommandInstructionSet{
		"start": {Procedures: []*domain.Procedure{{ExpectedPorts: []domain.ExpectedPort{{Name: "http", KeepAliveTraffic: "1b/5m"}}}}},
	}, []domain.Port{{Name: "http", Port: 80, Protocol: "tcp"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Source != "hubble-relay-unavailable" {
		t.Fatalf("source = %s, want hubble-relay-unavailable", status.Source)
	}
	if status.Traffic || status.TrafficOK != nil {
		t.Fatalf("traffic should be unavailable: %#v", status)
	}
}

func TestMaterializationRequiresPullImage(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset(), fakeHubble{})
	_, err := backend.MaterializeScroll(context.Background(), "ghcr.io/example/scroll:latest", "")
	if err == nil {
		t.Fatal("MaterializeScroll error = nil, want missing pull image error")
	}
	if !strings.Contains(err.Error(), "pull image is required") {
		t.Fatalf("error = %v, want pull image required", err)
	}
}

func TestSignalDeletesPersistentStatefulSetAndPods(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{})
	dataRoot := ref("druid", "druid-static-web-data")
	name := statefulSetName(dataRoot, "start")
	labels := baseLabels("druid-static-web-data")
	labels[labelProcedure] = "start"
	if _, err := client.AppsV1().StatefulSets("druid").Create(context.Background(), &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "druid", Labels: labels},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("druid").Create(context.Background(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "start-0", Namespace: "druid", Labels: labels},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := backend.Signal("", "start", "SIGKILL", dataRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AppsV1().StatefulSets("druid").Get(context.Background(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("StatefulSet get error = %v, want not found", err)
	}
	if _, err := client.CoreV1().Pods("druid").Get(context.Background(), "start-0", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Pod get error = %v, want not found", err)
	}
}
