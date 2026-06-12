package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
)

func setStatsReader(backend *Backend, nodeName string, summary *nodeStatsSummary, err error) {
	backend.statsReader = func(ctx context.Context, node string) (*nodeStatsSummary, error) {
		if err != nil {
			return nil, err
		}
		if node != nodeName {
			return nil, fmt.Errorf("unexpected node %s", node)
		}
		return summary, nil
	}
}

func podStats(namespace string, uid string, rx uint64, tx uint64) *nodeStatsSummary {
	rxCopy := rx
	txCopy := tx
	return &nodeStatsSummary{Pods: []nodePodStats{{
		PodRef: struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			UID       string `json:"uid"`
		}{Name: "runtime-pod", Namespace: namespace, UID: uid},
		Network: &nodeNetworkStats{RXBytes: &rxCopy, TXBytes: &txCopy},
	}}}
}

func runningProcedurePod(namespace string, root string, command string, procedure string, attempt int, uid string, jobName string) *corev1.Pod {
	labels := procedureTestLabels(root, command, procedure, attempt)
	if jobName != "" {
		labels["batch.kubernetes.io/job-name"] = jobName
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "runtime-pod",
			Namespace: namespace,
			UID:       typesUID(uid),
			Labels:    labels,
		},
		Spec:   corev1.PodSpec{NodeName: "node-a"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func typesUID(value string) types.UID {
	return types.UID(value)
}

func ptrString(value string) *string {
	return &value
}

func TestRootRefUsesRequestedNamespace(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset())
	if got, want := backend.RootRef("deployment-123", "games"), ref("games", dataPVCName("deployment-123")); got != want {
		t.Fatalf("RootRef = %s, want %s", got, want)
	}
	if got, want := backend.RootRef("deployment-123", ""), ref("druid", dataPVCName("deployment-123")); got != want {
		t.Fatalf("RootRef default = %s, want %s", got, want)
	}
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

	job, err := procedureJobSpec("druid", ref("druid", "druid-static-web-data"), "start", "start", "static-web-start-0", 1, procedure, nil, procedure.Env, "registry-secret")
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
	job, err := procedureJobSpec("druid", ref("druid", "druid-static-web-data"), "start", "start", "static-web-start-0", 1, procedure, nil, map[string]string{
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

func TestProcedureJobSpecAddsTCPReadinessProbe(t *testing.T) {
	procedure := &domain.Procedure{
		Image:         "itzg/minecraft-server",
		ExpectedPorts: []domain.ExpectedPort{{Name: "main"}},
	}
	job, err := procedureJobSpec("druid", ref("druid", "druid-minecraft-data"), "start", "start", "minecraft-start-0", 1, procedure, []domain.Port{{Name: "main", Protocol: "tcp", Port: 25565}}, nil, "registry-secret")
	if err != nil {
		t.Fatal(err)
	}
	if probe := job.Spec.Template.Spec.Containers[0].ReadinessProbe; probe == nil || probe.TCPSocket == nil || probe.TCPSocket.Port.IntVal != 25565 {
		t.Fatalf("readiness probe = %#v, want tcp 25565", probe)
	}
}

func TestProcedureReadinessProbeSkipsUDPOnlyPorts(t *testing.T) {
	procedure := &domain.Procedure{
		Image:         "steam",
		ExpectedPorts: []domain.ExpectedPort{{Name: "query"}},
	}
	statefulSet, err := procedureStatefulSetSpec("druid", ref("druid", "druid-game-data"), "start", "start", "game-start-0", procedure, []domain.Port{{Name: "query", Protocol: "udp", Port: 27015}}, nil, "registry-secret")
	if err != nil {
		t.Fatal(err)
	}
	if probe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe; probe != nil {
		t.Fatalf("readiness probe = %#v, want nil for udp-only ports", probe)
	}
}

func TestProcedureReadinessProbeUsesFirstTCPExpectedPort(t *testing.T) {
	procedure := &domain.Procedure{
		Image:         "steam",
		ExpectedPorts: []domain.ExpectedPort{{Name: "query"}, {Name: "rcon"}},
	}
	statefulSet, err := procedureStatefulSetSpec("druid", ref("druid", "druid-game-data"), "start", "start", "game-start-0", procedure, []domain.Port{
		{Name: "query", Protocol: "udp", Port: 27015},
		{Name: "rcon", Protocol: "tcp", Port: 27020},
	}, nil, "registry-secret")
	if err != nil {
		t.Fatal(err)
	}
	if probe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe; probe == nil || probe.TCPSocket == nil || probe.TCPSocket.Port.IntVal != 27020 {
		t.Fatalf("readiness probe = %#v, want tcp 27020", probe)
	}
}

func TestProcedureStatefulSetSpecUsesProvidedRuntimeEnv(t *testing.T) {
	procedure := &domain.Procedure{
		Image: "nginx:1.27",
		Env: map[string]string{
			"PROCEDURE_ONLY": "ignored",
		},
	}
	statefulSet, err := procedureStatefulSetSpec("druid", ref("druid", "druid-static-web-data"), "start", "start", "static-web-start-0", procedure, nil, map[string]string{
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

	statefulSet, err := procedureStatefulSetSpec("druid", ref("druid", "druid-static-web-data"), "start", "start", "static-web-start-0", procedure, []domain.Port{{Name: "http", Protocol: "tcp", Port: 8080}}, procedure.Env, "registry-secret")
	if err != nil {
		t.Fatal(err)
	}

	if statefulSet.Namespace != "druid" {
		t.Fatalf("namespace = %s, want druid", statefulSet.Namespace)
	}
	if statefulSet.Name != "static-web-start-0" {
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
	if probe := pod.Containers[0].ReadinessProbe; probe == nil || probe.TCPSocket == nil || probe.TCPSocket.Port.IntVal != 8080 {
		t.Fatalf("readiness probe = %#v, want tcp 8080", probe)
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
	for _, want := range []string{"druid --config /tmp/druid-registry.json", "worker pull", "--mode update", "--runtime-id deployment-123", "--callback-url", "chown -R 1000:1000"} {
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
	if env[workerPullRootEnvName] != "/scroll" {
		t.Fatalf("%s = %q, want /scroll", workerPullRootEnvName, env[workerPullRootEnvName])
	}
	if container.SecurityContext == nil || container.SecurityContext.RunAsUser == nil || *container.SecurityContext.RunAsUser != 0 {
		t.Fatalf("worker pull must run as root to repair PVC ownership, securityContext = %#v", container.SecurityContext)
	}
	if container.SecurityContext.RunAsGroup == nil || *container.SecurityContext.RunAsGroup != 0 {
		t.Fatalf("worker pull runAsGroup = %#v, want 0", container.SecurityContext.RunAsGroup)
	}
	if container.SecurityContext.RunAsNonRoot == nil || *container.SecurityContext.RunAsNonRoot {
		t.Fatalf("worker pull runAsNonRoot = %#v, want false", container.SecurityContext.RunAsNonRoot)
	}
	if len(job.Spec.Template.Spec.ImagePullSecrets) != 1 || job.Spec.Template.Spec.ImagePullSecrets[0].Name != "pull-secret" {
		t.Fatalf("image pull secrets = %#v", job.Spec.Template.Spec.ImagePullSecrets)
	}
}

func TestSpawnPullWorkerCreateUsesFinalPVCAndWorkerJob(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid", PullImage: "druid-cli:test"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	var jobs []*batchv1.Job
	backend.jobLogRunner = func(ctx context.Context, job *batchv1.Job) ([]byte, error) {
		jobs = append(jobs, job.DeepCopy())
		return nil, nil
	}
	action := ports.RuntimeWorkerAction{
		Mode:          ports.RuntimeWorkerModeCreate,
		RuntimeID:     "deployment-123",
		Artifact:      "registry.local/lab:1.0",
		Storage:       "25Gi",
		RootRef:       ref("games", dataPVCName("deployment-123")),
		MountPath:     "/scroll",
		CallbackURL:   "http://druid-cli:8083/internal/v1/workers/deployment-123/complete",
		CallbackToken: "secret-token",
	}
	if err := backend.SpawnPullWorker(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims("games").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pvcs.Items) != 1 || pvcs.Items[0].Name != dataPVCName("deployment-123") {
		t.Fatalf("pvcs = %#v, want final PVC", pvcs.Items)
	}
	if got := pvcs.Items[0].Spec.Resources.Requests.Storage().String(); got != "25Gi" {
		t.Fatalf("pvc storage = %s, want 25Gi", got)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	if jobs[0].Namespace != "games" {
		t.Fatalf("job namespace = %s, want games", jobs[0].Namespace)
	}
	command := strings.Join(jobs[0].Spec.Template.Spec.Containers[0].Command, " ")
	if !strings.Contains(command, "worker pull") || strings.Contains(command, "read-scroll") || strings.Contains(command, "--action-id") {
		t.Fatalf("command = %#v", jobs[0].Spec.Template.Spec.Containers[0].Command)
	}
}

func TestDeleteFinishedJobRemovesJob(t *testing.T) {
	client := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "finished", Namespace: "druid"},
	})
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)

	backend.deleteFinishedJob(context.Background(), "druid", "finished")

	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), "finished", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Job get error = %v, want not found", err)
	}
}

func TestCreateFreshJobKeepsFailedJob(t *testing.T) {
	client := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: "druid"},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type:   batchv1.JobFailed,
			Status: corev1.ConditionTrue,
		}}},
	})
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)

	created, err := backend.createFreshJob(context.Background(), &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: "druid"}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name == "failed" {
		t.Fatal("retry job reused failed job name")
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), "failed", metav1.GetOptions{}); err != nil {
		t.Fatalf("failed Job was not retained: %v", err)
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), created.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("retry Job was not created: %v", err)
	}
}

func TestCreateOrReuseProcedureJobRetainsFailedBaseAndCreatesRetry(t *testing.T) {
	root := ref("druid", dataPVCName("deployment-123"))
	base := procedureResourceName(root, "start", 1)
	client := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      base,
			Namespace: "druid",
			Labels:    procedureTestLabels(root, "start", "start.1", 1),
		},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type:   batchv1.JobFailed,
			Status: corev1.ConditionTrue,
		}}},
	})
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)

	created, err := backend.createOrReuseProcedureJob(context.Background(), "druid", root, "start", "start.1", base, &domain.Procedure{Image: "alpine"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != base+"-r2" {
		t.Fatalf("retry job name = %s, want %s-r2", created.Name, base)
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), base, metav1.GetOptions{}); err != nil {
		t.Fatalf("failed Job was not retained: %v", err)
	}
}

func TestCreateOrReuseProcedureJobUsesNextRetryAttempt(t *testing.T) {
	root := ref("druid", dataPVCName("deployment-123"))
	base := procedureResourceName(root, "start", 1)
	client := fake.NewSimpleClientset(
		failedProcedureJob(root, base, "start", "start.1", 1),
		failedProcedureJob(root, base+"-r2", "start", "start.1", 2),
	)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)

	created, err := backend.createOrReuseProcedureJob(context.Background(), "druid", root, "start", "start.1", base, &domain.Procedure{Image: "alpine"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != base+"-r3" {
		t.Fatalf("retry job name = %s, want %s-r3", created.Name, base)
	}
}

func TestCreateOrReuseProcedureJobReusesActiveAttempt(t *testing.T) {
	root := ref("druid", dataPVCName("deployment-123"))
	base := procedureResourceName(root, "start", 0)
	active := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      base,
			Namespace: "druid",
			Labels:    procedureTestLabels(root, "start", "coldstart", 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	client := fake.NewSimpleClientset(active)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)

	created, err := backend.createOrReuseProcedureJob(context.Background(), "druid", root, "start", "coldstart", base, &domain.Procedure{Image: "alpine"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != base {
		t.Fatalf("job name = %s, want existing %s", created.Name, base)
	}
	jobs, err := client.BatchV1().Jobs("druid").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs.Items))
	}
}

func TestResumeRestartProcedureIndexDeletesSupersededActiveProcedure(t *testing.T) {
	root := ref("druid", dataPVCName("deployment-123"))
	coldstart := "coldstart"
	start := "start"
	coldstartJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      procedureResourceName(root, "start", 0),
			Namespace: "druid",
			Labels:    procedureTestLabels(root, "start", coldstart, 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	startJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      procedureResourceName(root, "start", 1),
			Namespace: "druid",
			Labels:    procedureTestLabels(root, "start", start, 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	client := fake.NewSimpleClientset(coldstartJob, startJob)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)

	resumeIndex, err := backend.resumeRestartProcedureIndex(context.Background(), root, "start", &domain.CommandInstructionSet{Procedures: []*domain.Procedure{
		{Id: &coldstart},
		{Id: &start},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resumeIndex != 1 {
		t.Fatalf("resumeIndex = %d, want 1", resumeIndex)
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), coldstartJob.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("coldstart job err = %v, want not found", err)
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), startJob.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("start job err = %v", err)
	}
}

func TestResumeRestartProcedureIndexKeepsOnlyActiveFirstProcedure(t *testing.T) {
	root := ref("druid", dataPVCName("deployment-123"))
	coldstart := "coldstart"
	coldstartJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      procedureResourceName(root, "start", 0),
			Namespace: "druid",
			Labels:    procedureTestLabels(root, "start", coldstart, 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	client := fake.NewSimpleClientset(coldstartJob)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)

	resumeIndex, err := backend.resumeRestartProcedureIndex(context.Background(), root, "start", &domain.CommandInstructionSet{Procedures: []*domain.Procedure{{Id: &coldstart}}})
	if err != nil {
		t.Fatal(err)
	}
	if resumeIndex != 0 {
		t.Fatalf("resumeIndex = %d, want 0", resumeIndex)
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), coldstartJob.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("coldstart job err = %v", err)
	}
}

func TestCreateOrReuseProcedureJobDeletesSucceededAttempt(t *testing.T) {
	root := ref("druid", dataPVCName("deployment-123"))
	base := procedureResourceName(root, "install", 0)
	client := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      base,
			Namespace: "druid",
			Labels:    procedureTestLabels(root, "install", "install", 1),
		},
		Status: batchv1.JobStatus{Succeeded: 1},
	})
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)

	created, err := backend.createOrReuseProcedureJob(context.Background(), "druid", root, "install", "install", base, &domain.Procedure{Image: "alpine"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != base {
		t.Fatalf("job name = %s, want clean base %s after deleting succeeded attempt", created.Name, base)
	}
}

func TestWaitForJobReportsPodFailureReason(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-start", Namespace: "druid"},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type:   batchv1.JobFailed,
			Status: corev1.ConditionTrue,
		}}},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-start-pod", Namespace: "druid", Labels: map[string]string{"job-name": "failed-start"}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name: "main",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				Reason:   "OOMKilled",
				ExitCode: 137,
			}},
		}}},
	}
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset(job, pod))

	exitCode, err := backend.waitForJob(context.Background(), "druid", "failed-start")
	if err == nil {
		t.Fatal("waitForJob error = nil, want failure")
	}
	if exitCode == nil || *exitCode != 137 {
		t.Fatalf("exitCode = %#v, want 137", exitCode)
	}
	if !strings.Contains(err.Error(), "OOMKilled") {
		t.Fatalf("error = %v, want OOMKilled detail", err)
	}
}

func TestWaitForJobUsesRecentSuccessfulDeletion(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset())
	backend.recordJobExit("druid", "finished", 0)

	exitCode, err := backend.waitForJob(context.Background(), "druid", "finished")
	if err != nil {
		t.Fatalf("waitForJob error = %v, want nil", err)
	}
	if exitCode == nil || *exitCode != 0 {
		t.Fatalf("exitCode = %#v, want 0", exitCode)
	}
}

func TestWaitForJobMissingWithoutRecentExitFails(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset())

	if _, err := backend.waitForJob(context.Background(), "druid", "missing"); !apierrors.IsNotFound(err) {
		t.Fatalf("waitForJob error = %v, want not found", err)
	}
}

func failedProcedureJob(root string, name string, command string, procedure string, attempt int) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "druid",
			Labels:    procedureTestLabels(root, command, procedure, attempt),
		},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type:   batchv1.JobFailed,
			Status: corev1.ConditionTrue,
		}}},
	}
}

func procedureTestLabels(root string, command string, procedure string, attempt int) map[string]string {
	_, pvc, _ := parseRef(root)
	labels := baseLabels(pvc)
	labels[labelCommand] = dnsLabel(command)
	labels[labelProcedure] = dnsLabel(procedure)
	labels[labelAttempt] = fmt.Sprintf("%d", attempt)
	return labels
}

func TestKubernetesJobFailedRequiresTerminalCondition(t *testing.T) {
	retrying := &batchv1.Job{Status: batchv1.JobStatus{Failed: 1, Active: 1}}
	if kubernetesJobFailed(retrying) {
		t.Fatal("job with failed pod but no terminal Failed condition should still be retryable")
	}
	failed := &batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
		Type:   batchv1.JobFailed,
		Status: corev1.ConditionTrue,
	}}}}
	if !kubernetesJobFailed(failed) {
		t.Fatal("job with terminal Failed condition should be failed")
	}
}

func TestStartupContainerFailureIgnoresSuccessfulTermination(t *testing.T) {
	exitCode, detail, ok := startupContainerFailure("coldstart-pod", corev1.ContainerStatus{
		Name: "main",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason:   "Completed",
			ExitCode: 0,
		}},
	})
	if ok {
		t.Fatalf("startupContainerFailure = (%d, %q, true), want no failure", exitCode, detail)
	}
}

func TestStartupContainerFailureReportsNonzeroTermination(t *testing.T) {
	exitCode, detail, ok := startupContainerFailure("coldstart-pod", corev1.ContainerStatus{
		Name: "main",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason:   "Error",
			ExitCode: 42,
		}},
	})
	if !ok {
		t.Fatal("startupContainerFailure ok = false, want failure")
	}
	if exitCode != 42 {
		t.Fatalf("exitCode = %d, want 42", exitCode)
	}
	if !strings.Contains(detail, "Error") || !strings.Contains(detail, "exit_code=42") {
		t.Fatalf("detail = %q, want termination reason and exit code", detail)
	}
}

func TestExpectedServicesUseRootNamespace(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid-system"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("games", dataPVCName("deployment-123"))
	procedure := &domain.Procedure{ExpectedPorts: []domain.ExpectedPort{{Name: "http"}}}

	err := backend.ensureExpectedServices(context.Background(), root, "start", "start", procedure, []domain.Port{{Name: "http", Port: 8080, Protocol: "tcp"}}, map[string]int{"http": 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Services("games").Get(context.Background(), serviceName(root, "start", "http"), metav1.GetOptions{}); err != nil {
		t.Fatalf("service in runtime namespace: %v", err)
	}
	if _, err := client.CoreV1().Services("druid-system").Get(context.Background(), serviceName(root, "start", "http"), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("service in backend namespace error = %v, want not found", err)
	}
}

func TestRegistryConfigSecretUsesDruidClientConfigShape(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
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

func TestExpectedPortsUsesPodStatsTraffic(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("druid", "druid-static-web-data")
	procedureName := "start"
	pod := runningProcedurePod("druid", root, "start", procedureName, 1, "pod-start-stats", "start-job")
	if _, err := client.CoreV1().Pods("druid").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	globalPodTrafficStore.record("pod-start-stats", 100, 50, time.Now().Add(-6*time.Minute))
	globalPodTrafficStore.record("pod-start-stats", 100, 50, time.Now().Add(-4*time.Minute))
	setStatsReader(backend, "node-a", podStats("druid", "pod-start-stats", 200, 75), nil)
	service, err := serviceSpec("druid", root, procedureName, serviceSelector(refPVCName(root), procedureName, procedureName, "http", map[string]int{"http": 1}), "http", domain.Port{Name: "http", Port: 80, Protocol: "tcp"})
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
	if status.Source != "kubernetes-pod-stats" {
		t.Fatalf("source = %s, want kubernetes-pod-stats", status.Source)
	}
	if status.RXBytes == nil || *status.RXBytes != 200 || status.TXBytes == nil || *status.TXBytes != 75 || status.TrafficBytes == nil || *status.TrafficBytes != 100 {
		t.Fatalf("byte counters = %#v", status)
	}
}

func TestExpectedPortsDegradesWhenPodStatsUnavailable(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("druid", "druid-static-web-data")
	pod := runningProcedurePod("druid", root, "start", "start", 1, "pod-start-unavailable", "start-job")
	if _, err := client.CoreV1().Pods("druid").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	setStatsReader(backend, "node-a", nil, errors.New("stats unavailable"))
	service, err := serviceSpec("druid", root, "start", serviceSelector(refPVCName(root), "start", "start", "http", map[string]int{"http": 1}), "http", domain.Port{Name: "http", Port: 80, Protocol: "tcp"})
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
		"start": {Procedures: []*domain.Procedure{{Id: ptrString("start"), ExpectedPorts: []domain.ExpectedPort{{Name: "http", KeepAliveTraffic: "1b/5m"}}}}},
	}, []domain.Port{{Name: "http", Port: 80, Protocol: "tcp"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Source != "kubernetes-pod-stats-unavailable" {
		t.Fatalf("source = %s, want kubernetes-pod-stats-unavailable", status.Source)
	}
	if status.Traffic || status.TrafficOK != nil {
		t.Fatalf("traffic should be unavailable: %#v", status)
	}
}

func TestExpectedPortsWithoutActivePodDoesNotBorrowTraffic(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("druid", "druid-static-web-data")
	procedureName := "start"
	unrelated := runningProcedurePod("druid", ref("druid", "other-scroll"), "start", procedureName, 1, "pod-other", "start-job")
	if _, err := client.CoreV1().Pods("druid").Create(context.Background(), unrelated, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	globalPodTrafficStore.record("pod-other", 100, 50, time.Now().Add(-6*time.Minute))
	setStatsReader(backend, "node-a", podStats("druid", "pod-other", 200, 75), nil)
	service, err := serviceSpec("druid", root, procedureName, serviceSelector(refPVCName(root), procedureName, procedureName, "http", map[string]int{"http": 1}), "http", domain.Port{Name: "http", Port: 80, Protocol: "tcp"})
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
		"start": {Procedures: []*domain.Procedure{{Id: &procedureName, ExpectedPorts: []domain.ExpectedPort{{Name: "http", KeepAliveTraffic: "1b/5m"}}}}},
	}, []domain.Port{{Name: "http", Port: 80, Protocol: "tcp"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Source != "kubernetes-service" {
		t.Fatalf("source = %s, want kubernetes-service", status.Source)
	}
	if !status.Bound {
		t.Fatalf("bound = false, want true: %#v", status)
	}
	if status.Traffic || status.TrafficOK != nil {
		t.Fatalf("traffic should be absent without an active matching pod: %#v", status)
	}
}

func TestKeepAliveTrafficStopsIdleRunningProcedure(t *testing.T) {
	root := ref("druid", "druid-static-web-data")
	pod := runningProcedurePod("druid", root, "start", "start", 1, "pod-idle-stop", "start-job")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "start-job",
			Namespace:         "druid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
			Labels:            procedureTestLabels(root, "start", "start", 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	client := fake.NewSimpleClientset(job, pod)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	globalPodTrafficStore.record("pod-idle-stop", 100, 50, time.Now().Add(-2*time.Minute))
	setStatsReader(backend, "node-a", podStats("druid", "pod-idle-stop", 100, 50), nil)
	stopper := backend.keepAliveTrafficIdleStopper("druid", root, "start", "start", &domain.Procedure{
		ExpectedPorts: []domain.ExpectedPort{{Name: "main", KeepAliveTraffic: "1b/1s"}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})
	if stopper == nil {
		t.Fatal("stopper = nil, want enforcement")
	}

	stopped, err := stopper(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("stopped = false, want true")
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), "start-job", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("job err = %v, want not found", err)
	}
}

func TestKeepAliveTrafficKeepsProcedureWhenTrafficPresent(t *testing.T) {
	root := ref("druid", "druid-static-web-data")
	pod := runningProcedurePod("druid", root, "start", "start", 1, "pod-traffic-present", "start-job")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "start-job",
			Namespace:         "druid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
			Labels:            procedureTestLabels(root, "start", "start", 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	client := fake.NewSimpleClientset(job, pod)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	globalPodTrafficStore.record("pod-traffic-present", 100, 50, time.Now().Add(-2*time.Minute))
	globalPodTrafficStore.record("pod-traffic-present", 100, 50, time.Now().Add(-500*time.Millisecond))
	setStatsReader(backend, "node-a", podStats("druid", "pod-traffic-present", 102, 55), nil)
	stopper := backend.keepAliveTrafficIdleStopper("druid", root, "start", "start", &domain.Procedure{
		ExpectedPorts: []domain.ExpectedPort{{Name: "main", KeepAliveTraffic: "1b/1s"}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})

	stopped, err := stopper(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("stopped = true, want false")
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), "start-job", metav1.GetOptions{}); err != nil {
		t.Fatalf("job err = %v", err)
	}
}

func TestKeepAliveTrafficKeepsProcedureWhenPodStatsUnavailable(t *testing.T) {
	root := ref("druid", "druid-static-web-data")
	pod := runningProcedurePod("druid", root, "start", "start", 1, "pod-stats-unavailable", "start-job")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "start-job",
			Namespace:         "druid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
			Labels:            procedureTestLabels(root, "start", "start", 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	client := fake.NewSimpleClientset(job, pod)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	setStatsReader(backend, "node-a", nil, errors.New("stats unavailable"))
	stopper := backend.keepAliveTrafficIdleStopper("druid", root, "start", "start", &domain.Procedure{
		ExpectedPorts: []domain.ExpectedPort{{Name: "main", KeepAliveTraffic: "1b/1s"}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})

	stopped, err := stopper(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("stopped = true, want false")
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), "start-job", metav1.GetOptions{}); err != nil {
		t.Fatalf("job err = %v", err)
	}
}

func TestKeepAliveTrafficKeepsProcedureWhilePodStatsWarmUp(t *testing.T) {
	root := ref("druid", "druid-static-web-data")
	pod := runningProcedurePod("druid", root, "start", "start", 1, "pod-stats-warmup", "start-job")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "start-job",
			Namespace:         "druid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
			Labels:            procedureTestLabels(root, "start", "start", 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	client := fake.NewSimpleClientset(job, pod)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	setStatsReader(backend, "node-a", podStats("druid", "pod-stats-warmup", 100, 50), nil)
	stopper := backend.keepAliveTrafficIdleStopper("druid", root, "start", "start", &domain.Procedure{
		ExpectedPorts: []domain.ExpectedPort{{Name: "main", KeepAliveTraffic: "1b/1s"}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})

	stopped, err := stopper(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("stopped = true, want false while pod stats sample window warms up")
	}
}

func TestKeepAliveTrafficWaitsForFullWindowBeforeStopping(t *testing.T) {
	root := ref("druid", "druid-static-web-data")
	pod := runningProcedurePod("druid", root, "start", "start", 1, "pod-full-window", "start-job")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "start-job",
			Namespace:         "druid",
			CreationTimestamp: metav1.NewTime(time.Now()),
			Labels:            procedureTestLabels(root, "start", "start", 1),
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	client := fake.NewSimpleClientset(job, pod)
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	globalPodTrafficStore.record("pod-full-window", 100, 50, time.Now().Add(-2*time.Minute))
	setStatsReader(backend, "node-a", podStats("druid", "pod-full-window", 100, 50), nil)
	stopper := backend.keepAliveTrafficIdleStopper("druid", root, "start", "start", &domain.Procedure{
		ExpectedPorts: []domain.ExpectedPort{{Name: "main", KeepAliveTraffic: "1b/1m"}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})

	stopped, err := stopper(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("stopped = true, want false before full window")
	}
	if _, err := client.BatchV1().Jobs("druid").Get(context.Background(), "start-job", metav1.GetOptions{}); err != nil {
		t.Fatalf("job err = %v", err)
	}
}

func TestKeepAliveTrafficDoesNotStopColdstarter(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset())
	stopper := backend.keepAliveTrafficIdleStopper("druid", ref("druid", "druid-static-web-data"), "start", "coldstart", &domain.Procedure{
		Command:       []string{"druid-coldstarter"},
		ExpectedPorts: []domain.ExpectedPort{{Name: "main", KeepAliveTraffic: "1b/1s"}},
	}, []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}})
	if stopper != nil {
		t.Fatal("stopper != nil, want coldstarter keepAliveTraffic to be reporting-only")
	}
}

func TestRoutingTargetsReturnStableBackendServices(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset())
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
	if target.Selector[labelScrollID] != "druid-static-web-data" || target.Selector[labelProcedure] != "web" {
		t.Fatalf("selector = %#v", target.Selector)
	}
	if webdav.ServiceName != serviceName(root, "dev", "webdav") || webdav.Port != 8084 || webdav.Protocol != "https" {
		t.Fatalf("webdav target = %#v", webdav)
	}
}

func TestRoutingTargetsCollapseColdstarterAndRuntimePort(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset())
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
	if mainTargets[0].Selector[labelCommand] != "start" || mainTargets[0].Selector[labelProcedure] != "coldstart" {
		t.Fatalf("selector = %#v", mainTargets[0].Selector)
	}
}

func TestExpectedServiceForSharedPortMovesToActiveProcedure(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("druid", "druid-minecraft-data")
	coldstart := &domain.Procedure{ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}}
	start := &domain.Procedure{ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}}
	ports := []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}}
	portUse := map[string]int{"main": 2}

	if err := backend.ensureExpectedServices(context.Background(), root, "start", "coldstart", coldstart, ports, portUse); err != nil {
		t.Fatal(err)
	}
	service, err := client.CoreV1().Services("druid").Get(context.Background(), serviceName(root, "start", "main"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if service.Spec.Selector[labelProcedure] != "coldstart" {
		t.Fatalf("coldstart selector = %#v", service.Spec.Selector)
	}

	if err := backend.ensureExpectedServices(context.Background(), root, "start", "start", start, ports, portUse); err != nil {
		t.Fatal(err)
	}
	service, err = client.CoreV1().Services("druid").Get(context.Background(), serviceName(root, "start", "main"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if service.Spec.Selector[labelProcedure] != "start" {
		t.Fatalf("start selector = %#v", service.Spec.Selector)
	}
}

func TestRoutingTargetsUseCurrentServiceSelector(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("druid", "druid-minecraft-data")
	coldstart := "coldstart"
	start := "start"
	ports := []domain.Port{{Name: "main", Port: 25565, Protocol: "tcp"}}
	portUse := map[string]int{"main": 2}
	if err := backend.ensureExpectedServices(context.Background(), root, "start", "start", &domain.Procedure{ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}}, ports, portUse); err != nil {
		t.Fatal(err)
	}

	targets, err := backend.RoutingTargets(root, map[string]*domain.CommandInstructionSet{
		"start": {Procedures: []*domain.Procedure{
			{Id: &coldstart, ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}},
			{Id: &start, ExpectedPorts: []domain.ExpectedPort{{Name: "main"}}},
		}},
	}, ports)
	if err != nil {
		t.Fatal(err)
	}

	var main *domain.RuntimeRoutingTarget
	for idx := range targets {
		if targets[idx].PortName == "main" {
			main = &targets[idx]
		}
	}
	if main == nil {
		t.Fatalf("targets = %#v", targets)
	}
	if main.Procedure != "start" || main.Selector[labelProcedure] != "start" {
		t.Fatalf("main target = %#v", main)
	}
}

func TestStopRuntimeDeletesWorkloadsButPreservesDataAndServices(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("druid", "druid-static-web-data")
	labels := baseLabels("druid-static-web-data")
	labels[labelProcedure] = "web"
	jobName := "static-web-web-0"
	statefulSetName := "static-web-web-0"
	service, err := serviceSpec("druid", root, "web", serviceSelector(refPVCName(root), "web", "web", "http", map[string]int{"http": 1}), "http", domain.Port{Name: "http", Port: 8080, Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	for _, create := range []func() error{
		func() error {
			_, err := client.CoreV1().PersistentVolumeClaims("druid").Create(context.Background(), pvcSpec("druid", "druid-static-web-data", "", ""), metav1.CreateOptions{})
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
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("druid", "druid-static-web-data")
	service, err := serviceSpec("druid", root, "web", serviceSelector(refPVCName(root), "web", "web", "http", map[string]int{"http": 1}), "http", domain.Port{Name: "http", Port: 8080, Protocol: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("druid").Create(context.Background(), pvcSpec("druid", "druid-static-web-data", "", ""), metav1.CreateOptions{}); err != nil {
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

func TestBackupJobSpecUsesRuntimePVCAndRegistryEnv(t *testing.T) {
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
}

func TestSpawnPullWorkerRequiresPullImage(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset())
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

func TestSpawnPullWorkerRejectsLocalArtifactPath(t *testing.T) {
	backend := NewWithClient(Config{Namespace: "druid", PullImage: "druid-cli:test"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), fake.NewSimpleClientset())
	err := backend.SpawnPullWorker(context.Background(), ports.RuntimeWorkerAction{
		Mode:      ports.RuntimeWorkerModeCreate,
		RuntimeID: "scroll",
		Artifact:  t.TempDir(),
		RootRef:   ref("druid", dataPVCName("scroll")),
	})
	if err == nil {
		t.Fatal("SpawnPullWorker error = nil, want local artifact rejection")
	}
	if !strings.Contains(err.Error(), "requires an OCI artifact reference") {
		t.Fatalf("error = %v, want OCI artifact reference error", err)
	}
}

func TestSignalDeletesPersistentStatefulSetAndPods(t *testing.T) {
	client := fake.NewSimpleClientset()
	backend := NewWithClient(Config{Namespace: "druid"}, coreservices.NewConsoleManager(coreservices.NewLogManager()), client)
	root := ref("druid", "druid-static-web-data")
	name := "static-web-start-0"
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
