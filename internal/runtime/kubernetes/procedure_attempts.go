package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

const recentJobExitTTL = 10 * time.Minute

func (b *Backend) createOrReuseProcedureJob(ctx context.Context, namespace string, root string, commandName string, procedureName string, baseName string, procedure *domain.Procedure, env map[string]string) (*batchv1.Job, error) {
	_, pvc, err := parseRef(root)
	if err != nil {
		return nil, err
	}
	selector := labels.SelectorFromSet(labels.Set{
		labelScrollID:  dnsLabel(pvc),
		labelCommand:   dnsLabel(commandName),
		labelProcedure: dnsLabel(procedureName),
	}).String()
	jobs, err := b.client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	sort.Slice(jobs.Items, func(i, j int) bool {
		return jobs.Items[i].CreationTimestamp.Before(&jobs.Items[j].CreationTimestamp)
	})
	nextAttempt := 1
	var active *batchv1.Job
	for idx := range jobs.Items {
		job := &jobs.Items[idx]
		if job.Status.Succeeded > 0 {
			if err := b.deleteJobAndWait(ctx, namespace, job.Name); err != nil {
				return nil, err
			}
			continue
		}
		attempt := procedureJobAttempt(job, baseName)
		if attempt >= nextAttempt {
			nextAttempt = attempt + 1
		}
		if kubernetesJobFailed(job) {
			logger.Log().Warn("Retaining failed Kubernetes procedure job for retry",
				zap.String("namespace", namespace),
				zap.String("job", job.Name),
				zap.String("command", commandName),
				zap.String("procedure", procedureName),
				zap.Int("attempt", attempt),
			)
			continue
		}
		active = job.DeepCopy()
	}
	if active != nil {
		logger.Log().Info("Reusing active Kubernetes procedure job",
			zap.String("namespace", namespace),
			zap.String("job", active.Name),
			zap.String("command", commandName),
			zap.String("procedure", procedureName),
			zap.Int("attempt", procedureJobAttempt(active, baseName)),
		)
		return active, nil
	}
	name := procedureAttemptName(baseName, nextAttempt)
	job, err := procedureJobSpec(namespace, root, commandName, procedureName, name, nextAttempt, procedure, env, b.config.RegistrySecret)
	if err != nil {
		return nil, err
	}
	created, err := b.client.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	logger.Log().Info("Created Kubernetes procedure job",
		zap.String("namespace", namespace),
		zap.String("job", created.Name),
		zap.String("uid", string(created.UID)),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.Int("attempt", nextAttempt),
	)
	return created, nil
}

func procedureAttemptName(baseName string, attempt int) string {
	if attempt <= 1 {
		return baseName
	}
	return dnsLabel(fmt.Sprintf("%s-r%d", baseName, attempt))
}

func procedureJobAttempt(job *batchv1.Job, baseName string) int {
	if value := job.Labels[labelAttempt]; value != "" {
		var attempt int
		if _, err := fmt.Sscanf(value, "%d", &attempt); err == nil && attempt > 0 {
			return attempt
		}
	}
	if strings.HasPrefix(job.Name, baseName+"-r") {
		var attempt int
		if _, err := fmt.Sscanf(strings.TrimPrefix(job.Name, baseName+"-r"), "%d", &attempt); err == nil && attempt > 0 {
			return attempt
		}
	}
	return 1
}

func (b *Backend) createFreshJob(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	propagation := metav1.DeletePropagationBackground
	deleteCtx, cancelDelete := context.WithTimeout(ctx, 30*time.Second)
	defer cancelDelete()
	existing, err := b.client.BatchV1().Jobs(job.Namespace).Get(deleteCtx, job.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Error("Failed to check Kubernetes job before create", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	if existing != nil && kubernetesJobFailed(existing) {
		original := job.Name
		job = job.DeepCopy()
		job.Name = dnsLabel(fmt.Sprintf("%s-%s", original, shortHash(fmt.Sprintf("%s-%d", original, time.Now().UnixNano()))))
		logger.Log().Warn("Retaining failed Kubernetes job and creating retry job", zap.String("namespace", job.Namespace), zap.String("failed_job", original), zap.String("retry_job", job.Name))
	}
	logger.Log().Debug("Deleting stale Kubernetes job before create", zap.String("namespace", job.Namespace), zap.String("job", job.Name))
	if err := b.client.BatchV1().Jobs(job.Namespace).Delete(deleteCtx, job.Name, metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Error("Failed to delete stale Kubernetes job before create", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	for {
		_, err := b.client.BatchV1().Jobs(job.Namespace).Get(deleteCtx, job.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil {
			logger.Log().Error("Failed to check stale Kubernetes job before create", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
			return nil, err
		}
		logger.Log().Debug("Waiting for stale Kubernetes job deletion", zap.String("namespace", job.Namespace), zap.String("job", job.Name))
		select {
		case <-deleteCtx.Done():
			logger.Log().Error("Timed out waiting for stale Kubernetes job deletion", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(deleteCtx.Err()))
			return nil, deleteCtx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	createdJob, err := b.client.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes job", zap.String("namespace", job.Namespace), zap.String("job", job.Name), zap.Error(err))
		return nil, err
	}
	logger.Log().Info("Created Kubernetes job", zap.String("namespace", job.Namespace), zap.String("job", createdJob.Name), zap.String("uid", string(createdJob.UID)))
	return createdJob, nil
}

func (b *Backend) deleteJobAndWait(ctx context.Context, namespace string, name string) error {
	propagation := metav1.DeletePropagationBackground
	deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := b.client.BatchV1().Jobs(namespace).Delete(deleteCtx, name, metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	for {
		_, err := b.client.BatchV1().Jobs(namespace).Get(deleteCtx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-deleteCtx.Done():
			return deleteCtx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (b *Backend) deleteFinishedJob(ctx context.Context, namespace string, name string) {
	b.recordJobExit(namespace, name, 0)
	propagation := metav1.DeletePropagationBackground
	deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	logger.Log().Debug("Deleting finished Kubernetes job", zap.String("namespace", namespace), zap.String("job", name))
	err := b.client.BatchV1().Jobs(namespace).Delete(deleteCtx, name, metav1.DeleteOptions{PropagationPolicy: &propagation})
	if apierrors.IsNotFound(err) {
		logger.Log().Debug("Finished Kubernetes job was already absent", zap.String("namespace", namespace), zap.String("job", name))
		return
	}
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Log().Warn("Failed to delete finished Kubernetes job", zap.String("namespace", namespace), zap.String("job", name), zap.Error(err))
		return
	}
	logger.Log().Info("Deleted finished Kubernetes job", zap.String("namespace", namespace), zap.String("job", name))
}

func (b *Backend) recordJobExit(namespace string, name string, exitCode int) {
	b.jobExitMu.Lock()
	defer b.jobExitMu.Unlock()
	if b.jobExits == nil {
		b.jobExits = make(map[string]recentJobExit)
	}
	now := time.Now()
	for key, item := range b.jobExits {
		if now.Sub(item.recordedAt) > recentJobExitTTL {
			delete(b.jobExits, key)
		}
	}
	b.jobExits[jobExitKey(namespace, name)] = recentJobExit{exitCode: exitCode, recordedAt: now}
}

func (b *Backend) recentJobExit(namespace string, name string) (*int, bool) {
	b.jobExitMu.Lock()
	defer b.jobExitMu.Unlock()
	item, ok := b.jobExits[jobExitKey(namespace, name)]
	if !ok {
		return nil, false
	}
	if time.Since(item.recordedAt) > recentJobExitTTL {
		delete(b.jobExits, jobExitKey(namespace, name))
		return nil, false
	}
	exitCode := item.exitCode
	return &exitCode, true
}

func jobExitKey(namespace string, name string) string {
	return namespace + "/" + name
}
