package kubernetes

import (
	"context"
	"encoding/json"
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
	"github.com/highcard-dev/daemon/internal/core/ports"
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
	job := pullJobSpec("druid", "pull", "scroll-pvc", "druid-cli:test", "registry:5000/lab:1.0", "", "", true)

	env := job.Spec.Template.Spec.Containers[0].Env
	if len(env) != 1 || env[0].Name != "DRUID_REGISTRY_PLAIN_HTTP" || env[0].Value != "true" {
		t.Fatalf("env = %#v", env)
	}
}

func TestPullJobSpecMountsRegistryConfigSecret(t *testing.T) {
	job := pullJobSpec("druid", "pull", "scroll-pvc", "druid-cli:test", "registry.local/lab:1.0", "", "runtime-registry", false)
	container := job.Spec.Template.Spec.Containers[0]
	if !strings.Contains(strings.Join(container.Command, " "), "--config /tmp/druid-registry.json") {
		t.Fatalf("command = %#v, want generated registry config", container.Command)
	}
	if len(container.Env) != 1 || container.Env[0].Name != registryConfigEnvName {
		t.Fatalf("env = %#v", container.Env)
	}
	ref := container.Env[0].ValueFrom.SecretKeyRef
	if ref == nil || ref.Name != "runtime-registry" || ref.Key != registryConfigSecretKey {
		t.Fatalf("secret ref = %#v", ref)
	}
}

func TestWorkerPullJobSpecRunsDruidWorkerPull(t *testing.T) {
	action := ports.RuntimeWorkerAction{
		Mode:          ports.RuntimeWorkerModeUpdate,
		RuntimeID:     "deployment-123",
		Artifact:      "registry.local/lab:2.0",
		MountPath:     "/scroll",
		CallbackURL:   "http://druid-cli:8083/internal/v1/workers/deployment-123/complete",
		CallbackToken: "secret-token",
	}
	job := workerPullJobSpec("druid", "worker-pull", "runtime-pvc", "druid-cli:test", action, "pull-secret", "runtime-registry", true)
	container := job.Spec.Template.Spec.Containers[0]
	command := strings.Join(container.Command, " ")
	for _, want := range []string{"druid --config /tmp/druid-registry.json", "worker pull", "--mode update", "--runtime-id deployment-123", "--callback-url"} {
		if !strings.Contains(command, want) {
			t.Fatalf("command = %#v, want %s", container.Command, want)
		}
	}
	if strings.Contains(command, "--action-id") {
		t.Fatalf("command = %#v, should not contain --action-id", container.Command)
	}
	env := map[string]string{}
	for _, item := range container.Env {
		env[item.Name] = item.Value
	}
	if env["DRUID_WORKER_TOKEN"] != "secret-token" || env["DRUID_REGISTRY_PLAIN_HTTP"] != "true" {
		t.Fatalf("env = %#v", container.Env)
	}
	if len(job.Spec.Template.Spec.ImagePullSecrets) != 1 || job.Spec.Template.Spec.ImagePullSecrets[0].Name != "pull-secret" {
		t.Fatalf("image pull secrets = %#v", job.Spec.Template.Spec.ImagePullSecrets)
	}
}

func TestSpawnPullWorkerCreateUsesFinalPVCAndWorkerJob(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid", PullImage: "druid-cli:test"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{})
	var jobs []*batchv1.Job
	backend.jobLogRunner = func(ctx context.Context, job *batchv1.Job) ([]byte, error) {
		jobs = append(jobs, job.DeepCopy())
		return nil, nil
	}
	action := ports.RuntimeWorkerAction{
		Mode:          ports.RuntimeWorkerModeCreate,
		RuntimeID:     "deployment-123",
		Artifact:      "registry.local/lab:1.0",
		RootRef:       ref("druid", dataPVCName("deployment-123")),
		MountPath:     "/scroll",
		CallbackURL:   "http://druid-cli:8083/internal/v1/workers/deployment-123/complete",
		CallbackToken: "secret-token",
	}
	if err := backend.SpawnPullWorker(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims("druid").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pvcs.Items) != 1 || pvcs.Items[0].Name != dataPVCName("deployment-123") {
		t.Fatalf("pvcs = %#v, want final PVC", pvcs.Items)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	command := strings.Join(jobs[0].Spec.Template.Spec.Containers[0].Command, " ")
	if !strings.Contains(command, "worker pull") || strings.Contains(command, "cat /scroll/scroll.yaml") || strings.Contains(command, "--action-id") {
		t.Fatalf("command = %#v", jobs[0].Spec.Template.Spec.Containers[0].Command)
	}
}

func TestRegistryConfigSecretUsesDruidClientConfigShape(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{})
	secretName, cleanup, err := backend.createRegistryConfigSecret(context.Background(), "druid", "artifact", []domain.RegistryCredential{{
		Host:     "artifacts.druid.gg/user/scroll",
		Username: "robot$scroll",
		Password: "secret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	secret, err := client.CoreV1().Secrets("druid").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Registries []domain.RegistryCredential `json:"registries"`
	}
	if err := json.Unmarshal(secret.Data[registryConfigSecretKey], &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Registries) != 1 || config.Registries[0].Host != "artifacts.druid.gg/user/scroll" || config.Registries[0].Username != "robot$scroll" {
		t.Fatalf("config = %#v", config)
	}
}

func TestExpectedPortsUsesHubbleFlowPresence(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{hasFlow: true})
	root := ref("druid", "druid-static-web-data")
	procedureName := "start"
	service, err := serviceSpec("druid", root, procedureName, "http", domain.Port{Name: "http", Port: 80, Protocol: "tcp"})
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

	statuses, err := backend.ExpectedPorts(root, map[string]*domain.CommandInstructionSet{
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
	root := ref("druid", "druid-static-web-data")
	service, err := serviceSpec("druid", root, "start", "http", domain.Port{Name: "http", Port: 80, Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Services("druid").Create(context.Background(), service, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	statuses, err := backend.ExpectedPorts(root, map[string]*domain.CommandInstructionSet{
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
	root := ref("druid", "druid-static-web-data")
	procedureID := "web"

	targets, err := backend.RoutingTargets(root, map[string]*domain.CommandInstructionSet{
		"serve": {Procedures: []*domain.Procedure{{
			Id:            &procedureID,
			ExpectedPorts: []domain.ExpectedPort{{Name: "http"}},
		}}},
	}, []domain.Port{{Name: "http", Port: 8080, Protocol: "http"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(targets) != 2 {
		t.Fatalf("targets = %#v", targets)
	}
	var target domain.RuntimeRoutingTarget
	var webdav domain.RuntimeRoutingTarget
	for _, item := range targets {
		if item.Name == "http" {
			target = item
		}
		if item.Name == "webdav" {
			webdav = item
		}
	}
	if target.Namespace != "druid" || target.ServiceName != serviceName(root, "web", "http") || target.ServicePort != 8080 {
		t.Fatalf("target = %#v", target)
	}
	if target.Protocol != "http" || target.PortName != "http" || target.Procedure != "web" {
		t.Fatalf("target = %#v", target)
	}
	if target.Selector[labelScrollID] != "druid-static-web-data" || target.Selector[labelPortName] != "http" {
		t.Fatalf("selector = %#v", target.Selector)
	}
	if webdav.ServiceName != serviceName(root, "dev", "webdav") || webdav.Port != 8084 || webdav.Protocol != "https" {
		t.Fatalf("webdav target = %#v", webdav)
	}
}

func TestRoutingTargetsCollapseColdstarterAndRuntimePort(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset(), fakeHubble{})
	root := ref("druid", "druid-minecraft-data")
	coldstart := "coldstart"
	start := "start"

	targets, err := backend.RoutingTargets(root, map[string]*domain.CommandInstructionSet{
		"start": {Procedures: []*domain.Procedure{
			{Id: &coldstart, ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}},
			{Id: &start, ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}},
		}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})
	if err != nil {
		t.Fatal(err)
	}

	var mainTargets []domain.RuntimeRoutingTarget
	for _, target := range targets {
		if target.PortName == "main" {
			mainTargets = append(mainTargets, target)
		}
	}
	if len(mainTargets) != 1 {
		t.Fatalf("main targets = %#v", mainTargets)
	}
	if mainTargets[0].Name != "main" || mainTargets[0].Procedure != "coldstart" {
		t.Fatalf("main target = %#v", mainTargets[0])
	}
	if mainTargets[0].Selector[labelPortName] != "main" {
		t.Fatalf("selector = %#v", mainTargets[0].Selector)
	}
}

func TestStopRuntimeDeletesWorkloadsButPreservesDataAndServices(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{})
	root := ref("druid", "druid-static-web-data")
	labels := baseLabels("druid-static-web-data")
	labels[labelProcedure] = "web"
	jobName := jobName("proc", root, "web")
	statefulSetName := statefulSetName(root, "web")
	service, err := serviceSpec("druid", root, "web", "http", domain.Port{Name: "http", Port: 8080, Protocol: "tcp"})
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

	if err := backend.StopRuntime(root); err != nil {
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
	root := ref("druid", "druid-static-web-data")
	service, err := serviceSpec("druid", root, "web", "http", domain.Port{Name: "http", Port: 8080, Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("druid").Create(context.Background(), pvcSpec("druid", "druid-static-web-data", ""), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Services("druid").Create(context.Background(), service, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := backend.DeleteRuntime(root, true); err != nil {
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
	backup := backupJobSpec("druid", "backup", "runtime-pvc", "druid-cli:test", "registry.local/scroll:backup", "registry-secret", "", true)
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

func TestSpawnPullWorkerRequiresPullImage(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset(), fakeHubble{})
	err := backend.SpawnPullWorker(context.Background(), ports.RuntimeWorkerAction{
		Mode:      ports.RuntimeWorkerModeCreate,
		RuntimeID: "scroll",
		Artifact:  "ghcr.io/example/scroll:latest",
		RootRef:   ref("druid", dataPVCName("scroll")),
	})
	if err == nil {
		t.Fatal("SpawnPullWorker error = nil, want missing pull image error")
	}
	if !strings.Contains(err.Error(), "pull image is required") {
		t.Fatalf("error = %v, want pull image required", err)
	}
}

func TestSignalDeletesPersistentStatefulSetAndPods(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client, fakeHubble{})
	root := ref("druid", "druid-static-web-data")
	name := statefulSetName(root, "start")
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

	if err := backend.Signal("", "start", "SIGKILL", root); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AppsV1().StatefulSets("druid").Get(context.Background(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("StatefulSet get error = %v, want not found", err)
	}
	if _, err := client.CoreV1().Pods("druid").Get(context.Background(), "start-0", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Pod get error = %v, want not found", err)
	}
}
