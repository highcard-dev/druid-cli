package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
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

	job, err := procedureJobSpec("druid", ref("druid", "druid-static-web-data"), "start", procedure, procedure.Env, "registry-secret")
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

func TestProcedureJobSpecUsesProvidedRuntimeEnv(t *testing.T) {
	procedure := &domain.Procedure{
		Image: "alpine:3.20",
		Env: map[string]string{
			"PROCEDURE_ONLY": "ignored",
		},
	}
	job, err := procedureJobSpec("druid", ref("druid", "druid-static-web-data"), "start", procedure, map[string]string{
		"DRUID_PORT_HTTP": "8080",
	}, "registry-secret")
	if err != nil {
		t.Fatal(err)
	}
	env := job.Spec.Template.Spec.Containers[0].Env
	if len(env) != 1 || env[0].Name != "DRUID_PORT_HTTP" || env[0].Value != "8080" {
		t.Fatalf("env = %#v", env)
	}
}

func TestProcedureStatefulSetSpecUsesProvidedRuntimeEnv(t *testing.T) {
	procedure := &domain.Procedure{
		Image: "nginx:1.27",
		Env: map[string]string{
			"PROCEDURE_ONLY": "ignored",
		},
	}
	statefulSet, err := procedureStatefulSetSpec("druid", ref("druid", "druid-static-web-data"), "start", procedure, map[string]string{
		"DRUID_PORT_HTTP": "8080",
	}, "registry-secret")
	if err != nil {
		t.Fatal(err)
	}
	env := statefulSet.Spec.Template.Spec.Containers[0].Env
	if len(env) != 1 || env[0].Name != "DRUID_PORT_HTTP" || env[0].Value != "8080" {
		t.Fatalf("env = %#v", env)
	}
}

func TestReadDataFileJobSpecScopesPathToScrollPVC(t *testing.T) {
	job := readDataFileJobSpec("druid", "read-file", "druid-scroll-data", "alpine:3.20", "data/private/dist/app.wasm")
	container := job.Spec.Template.Spec.Containers[0]
	if job.Namespace != "druid" || job.Labels[labelComponent] != "read-data-file" {
		t.Fatalf("unexpected job metadata: namespace=%s labels=%#v", job.Namespace, job.Labels)
	}
	if got := container.Command; len(got) != 2 || got[0] != "cat" || got[1] != "/scroll/data/private/dist/app.wasm" {
		t.Fatalf("unexpected command: %#v", got)
	}
	if got := job.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName; got != "druid-scroll-data" {
		t.Fatalf("claim = %s, want druid-scroll-data", got)
	}
}

func TestWriteDataFileJobSpecScopesPathToScrollPVC(t *testing.T) {
	job := writeDataFileJobSpec("druid", "write-file", "druid-scroll-data", "alpine:3.20", "data/private/config.json", "e30=")
	container := job.Spec.Template.Spec.Containers[0]
	if job.Namespace != "druid" || job.Labels[labelComponent] != "write-data-file" {
		t.Fatalf("unexpected job metadata: namespace=%s labels=%#v", job.Namespace, job.Labels)
	}
	if got := container.Command; len(got) != 5 || got[4] != "/scroll/data/private/config.json" {
		t.Fatalf("unexpected command: %#v", got)
	}
	if len(container.Env) != 1 || container.Env[0].Name != "DRUID_DATA_FILE_B64" || container.Env[0].Value != "e30=" {
		t.Fatalf("unexpected env: %#v", container.Env)
	}
}

func TestProcedureStatefulSetSpecBuildsPersistentWorkload(t *testing.T) {
	procedure := &domain.Procedure{
		Image:         "nginx:1.27",
		Command:       []string{"nginx", "-g", "daemon off;"},
		ExpectedPorts: []domain.ExpectedPort{{Name: "http"}},
		Mounts:        []domain.Mount{{Path: "/usr/share/nginx/html", SubPath: "site", ReadOnly: true}},
	}

	statefulSet, err := procedureStatefulSetSpec("druid", ref("druid", "druid-static-web-data"), "start", procedure, procedure.Env, "registry-secret")
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

func TestRoutingTargetsReturnStableBackendServices(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset(), fakeHubble{})
	dataRoot := ref("druid", "druid-static-web-data")
	procedureID := "web"

	targets, err := backend.RoutingTargets(dataRoot, map[string]*domain.CommandInstructionSet{
		"serve": {Procedures: []*domain.Procedure{{
			Id:            &procedureID,
			ExpectedPorts: []domain.ExpectedPort{{Name: "http"}},
		}}},
	}, []domain.Port{{Name: "http", Port: 8080, Protocol: "http"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(targets) != 1 {
		t.Fatalf("targets = %#v", targets)
	}
	target := targets[0]
	if target.Namespace != "druid" || target.ServiceName != serviceName(dataRoot, "web", "http") || target.ServicePort != 8080 {
		t.Fatalf("target = %#v", target)
	}
	if target.Protocol != "http" || target.PortName != "http" || target.Procedure != "web" {
		t.Fatalf("target = %#v", target)
	}
	if target.Selector[labelScrollID] != "druid-static-web-data" || target.Selector[labelProcedure] != "web" {
		t.Fatalf("selector = %#v", target.Selector)
	}
}

func TestStopRuntimeDeletesWorkloadsButPreservesDataAndServices(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{})
	dataRoot := ref("druid", "druid-static-web-data")
	labels := baseLabels("druid-static-web-data")
	labels[labelProcedure] = "web"
	jobName := jobName("proc", dataRoot, "web")
	statefulSetName := statefulSetName(dataRoot, "web")
	service, err := serviceSpec("druid", dataRoot, "web", "http", domain.Port{Name: "http", Port: 8080, Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	for _, create := range []func() error{
		func() error {
			_, err := client.CoreV1().PersistentVolumeClaims("druid").Create(context.Background(), pvcSpec("druid", "druid-static-web-data", ""), metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := client.CoreV1().Services("druid").Create(context.Background(), service, metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := client.BatchV1().Jobs("druid").Create(context.Background(), &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: "druid", Labels: labels}}, metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := client.AppsV1().StatefulSets("druid").Create(context.Background(), &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: statefulSetName, Namespace: "druid", Labels: labels}}, metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := client.CoreV1().Pods("druid").Create(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "druid", Labels: labels}}, metav1.CreateOptions{})
			return err
		},
	} {
		if err := create(); err != nil {
			t.Fatal(err)
		}
	}

	if err := backend.StopRuntime(dataRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), jobName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Job get error = %v, want not found", err)
	}
	if _, err := client.AppsV1().StatefulSets("druid").Get(context.Background(), statefulSetName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("StatefulSet get error = %v, want not found", err)
	}
	if _, err := client.CoreV1().Pods("druid").Get(context.Background(), "web-0", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Pod get error = %v, want not found", err)
	}
	if _, err := client.CoreV1().Services("druid").Get(context.Background(), service.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("Service get error = %v, want preserved", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("druid").Get(context.Background(), "druid-static-web-data", metav1.GetOptions{}); err != nil {
		t.Fatalf("PVC get error = %v, want preserved", err)
	}
}

func TestDeleteRuntimePurgesServicesAndDataWhenRequested(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{})
	dataRoot := ref("druid", "druid-static-web-data")
	service, err := serviceSpec("druid", dataRoot, "web", "http", domain.Port{Name: "http", Port: 8080, Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("druid").Create(context.Background(), pvcSpec("druid", "druid-static-web-data", ""), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Services("druid").Create(context.Background(), service, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := backend.DeleteRuntime(dataRoot, true); err != nil {
		t.Fatal(err)
	}

	if _, err := client.CoreV1().Services("druid").Get(context.Background(), service.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Service get error = %v, want not found", err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("druid").Get(context.Background(), "druid-static-web-data", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("PVC get error = %v, want not found", err)
	}
}

func TestBackupAndRestoreJobSpecsUseRuntimePVCAndRegistryEnv(t *testing.T) {
	backup := backupJobSpec("druid", "backup", "runtime-pvc", "druid-client:test", "registry.local/scroll:backup", "registry-secret", true)
	if backup.Spec.Template.Spec.Containers[0].Command[1] != "push" {
		t.Fatalf("backup command = %#v", backup.Spec.Template.Spec.Containers[0].Command)
	}
	if got := backup.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName; got != "runtime-pvc" {
		t.Fatalf("backup PVC = %s, want runtime-pvc", got)
	}
	if len(backup.Spec.Template.Spec.ImagePullSecrets) != 1 || backup.Spec.Template.Spec.ImagePullSecrets[0].Name != "registry-secret" {
		t.Fatalf("image pull secrets = %#v", backup.Spec.Template.Spec.ImagePullSecrets)
	}
	if env := backup.Spec.Template.Spec.Containers[0].Env; len(env) != 1 || env[0].Name != "DRUID_REGISTRY_PLAIN_HTTP" || env[0].Value != "true" {
		t.Fatalf("env = %#v", env)
	}

	restore := replacePVCJobSpec("druid", "restore", "stage-pvc", "runtime-pvc", "alpine:3.20")
	if got := restore.Labels[labelComponent]; got != "restore-scroll" {
		t.Fatalf("restore component = %s", got)
	}
	if command := strings.Join(restore.Spec.Template.Spec.Containers[0].Command, " "); !strings.Contains(command, "rm -rf") || !strings.Contains(command, "cp -a") {
		t.Fatalf("restore command = %#v", restore.Spec.Template.Spec.Containers[0].Command)
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
