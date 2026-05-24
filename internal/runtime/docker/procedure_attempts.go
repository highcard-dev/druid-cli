package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type dockerSelectedProcedureContainer struct {
	ID    string
	Name  string
	Start bool
}

func (b *Backend) createOrReuseProcedureContainer(ctx context.Context, root string, commandName string, procedureName string, resourceName string, config *container.Config, hostConfig *container.HostConfig) (dockerSelectedProcedureContainer, error) {
	selector := filters.NewArgs(
		filters.Arg("label", dockerLabelRole+"="+dockerRoleProcedure),
		filters.Arg("label", dockerLabelRootHash+"="+rootHash(root)),
		filters.Arg("label", dockerLabelResource+"="+resourceName),
	)
	items, err := b.client.ContainerList(ctx, container.ListOptions{All: true, Filters: selector})
	if err != nil {
		return dockerSelectedProcedureContainer{}, err
	}
	attempts := make([]dockerProcedureAttempt, 0, len(items))
	for _, item := range items {
		inspected, inspectErr := b.client.ContainerInspect(ctx, item.ID)
		if inspectErr != nil {
			return dockerSelectedProcedureContainer{}, inspectErr
		}
		state := inspected.State
		exitCode := 1
		running := false
		start := false
		if state != nil {
			exitCode = state.ExitCode
			running = state.Running || state.Restarting || state.Paused || state.Status == container.StateCreated
			start = state.Status == container.StateCreated
		}
		attempts = append(attempts, dockerProcedureAttempt{
			ID:       item.ID,
			Name:     dockerContainerName(item),
			Attempt:  dockerProcedureAttemptNumber(item.Labels, dockerContainerName(item), ContainerName(root, resourceName)),
			Created:  item.Created,
			ExitCode: exitCode,
			Running:  running,
			Start:    start,
		})
	}
	if active := activeDockerProcedureAttempt(attempts); active != nil {
		logger.Log().Info("Reusing active Docker procedure container",
			zap.String("container", active.Name),
			zap.String("container_id", active.ID),
			zap.String("command", commandName),
			zap.String("procedure", procedureName),
			zap.Int("attempt", active.Attempt),
		)
		return dockerSelectedProcedureContainer{ID: active.ID, Name: active.Name, Start: active.Start}, nil
	}
	for _, item := range successfulDockerProcedureAttempts(attempts) {
		logger.Log().Info("Deleting successful Docker procedure container",
			zap.String("container", item.Name),
			zap.String("container_id", item.ID),
			zap.String("command", commandName),
			zap.String("procedure", procedureName),
			zap.Int("attempt", item.Attempt),
		)
		_ = b.client.ContainerRemove(ctx, item.ID, container.RemoveOptions{Force: true})
	}
	for _, item := range prunedDockerProcedureAttempts(attempts, dockerFailedProcedureRetention) {
		logger.Log().Info("Pruning old failed Docker procedure container",
			zap.String("container", item.Name),
			zap.String("container_id", item.ID),
			zap.String("command", commandName),
			zap.String("procedure", procedureName),
			zap.Int("attempt", item.Attempt),
		)
		_ = b.client.ContainerRemove(ctx, item.ID, container.RemoveOptions{Force: true})
	}
	attempt := nextDockerProcedureAttempt(attempts)
	name := dockerProcedureAttemptName(ContainerName(root, resourceName), attempt)
	config = copyContainerConfig(config)
	config.Labels = dockerProcedureLabels(root, commandName, procedureName, resourceName, attempt, config.Labels)
	created, err := b.client.ContainerCreate(ctx, config, hostConfig, nil, nil, name)
	if err != nil {
		return dockerSelectedProcedureContainer{}, err
	}
	logger.Log().Info("Created Docker procedure container",
		zap.String("container", name),
		zap.String("container_id", created.ID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.Int("attempt", attempt),
	)
	return dockerSelectedProcedureContainer{ID: created.ID, Name: name, Start: true}, nil
}

func (b *Backend) containerFailureError(ctx context.Context, id string, name string, imageRef string, exitCode int) error {
	inspected, inspectErr := b.client.ContainerInspect(ctx, id)
	oomKilled := false
	dockerError := ""
	if inspectErr == nil && inspected.State != nil {
		oomKilled = inspected.State.OOMKilled
		dockerError = inspected.State.Error
	}
	tail := b.containerLogTail(ctx, id, 4096)
	return errors.New(dockerContainerFailureMessage(name, id, imageRef, exitCode, oomKilled, dockerError, tail))
}

func dockerContainerFailureMessage(name string, id string, imageRef string, exitCode int, oomKilled bool, dockerError string, tail string) string {
	message := fmt.Sprintf("docker container %s (%s, image %s) exited with code %d", name, id, imageRef, exitCode)
	if oomKilled {
		message += " oom_killed=true"
	}
	if dockerError != "" {
		message += ": " + dockerError
	}
	if tail != "" {
		message += ": " + tail
	}
	return message
}

func (b *Backend) containerLogTail(ctx context.Context, id string, limit int) string {
	logs, err := b.client.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: "80"})
	if err != nil {
		return ""
	}
	defer logs.Close()
	var out strings.Builder
	_, _ = io.Copy(&out, logs)
	text := strings.TrimSpace(out.String())
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}

func copyContainerConfig(config *container.Config) *container.Config {
	clone := *config
	if config.Labels != nil {
		clone.Labels = map[string]string{}
		for key, value := range config.Labels {
			clone.Labels[key] = value
		}
	}
	return &clone
}

func dockerProcedureLabels(root string, commandName string, procedureName string, resourceName string, attempt int, base map[string]string) map[string]string {
	labels := map[string]string{}
	for key, value := range base {
		labels[key] = value
	}
	labels[dockerLabelRole] = dockerRoleProcedure
	labels[dockerLabelRuntimeID] = runtimeID(root)
	labels[dockerLabelRootHash] = rootHash(root)
	labels[dockerLabelCommand] = commandName
	labels[dockerLabelProcedure] = procedureName
	labels[dockerLabelResource] = resourceName
	if attempt > 0 {
		labels[dockerLabelAttempt] = strconv.Itoa(attempt)
	}
	return labels
}

type dockerProcedureAttempt struct {
	ID       string
	Name     string
	Attempt  int
	Created  int64
	ExitCode int
	Running  bool
	Start    bool
}

func activeDockerProcedureAttempt(items []dockerProcedureAttempt) *dockerProcedureAttempt {
	var active *dockerProcedureAttempt
	for idx := range items {
		if items[idx].Running {
			if active == nil || items[idx].Attempt > active.Attempt || (items[idx].Attempt == active.Attempt && items[idx].Created > active.Created) {
				active = &items[idx]
			}
		}
	}
	return active
}

func successfulDockerProcedureAttempts(items []dockerProcedureAttempt) []dockerProcedureAttempt {
	var result []dockerProcedureAttempt
	for _, item := range items {
		if !item.Running && item.ExitCode == 0 {
			result = append(result, item)
		}
	}
	return result
}

func prunedDockerProcedureAttempts(items []dockerProcedureAttempt, retain int) []dockerProcedureAttempt {
	var failed []dockerProcedureAttempt
	for _, item := range items {
		if !item.Running && item.ExitCode != 0 {
			failed = append(failed, item)
		}
	}
	sort.Slice(failed, func(i, j int) bool {
		if failed[i].Attempt == failed[j].Attempt {
			return failed[i].Created < failed[j].Created
		}
		return failed[i].Attempt < failed[j].Attempt
	})
	if len(failed) <= retain {
		return nil
	}
	return failed[:len(failed)-retain]
}

func nextDockerProcedureAttempt(items []dockerProcedureAttempt) int {
	next := 1
	for _, item := range items {
		if item.Attempt >= next {
			next = item.Attempt + 1
		}
	}
	return next
}

func dockerProcedureAttemptName(baseName string, attempt int) string {
	if attempt <= 1 {
		return baseName
	}
	return sanitizeContainerName(fmt.Sprintf("%s-r%d", baseName, attempt))
}

func dockerProcedureAttemptNumber(labels map[string]string, name string, baseName string) int {
	if value := labels[dockerLabelAttempt]; value != "" {
		if attempt, err := strconv.Atoi(value); err == nil && attempt > 0 {
			return attempt
		}
	}
	if strings.HasPrefix(name, baseName+"-r") {
		if attempt, err := strconv.Atoi(strings.TrimPrefix(name, baseName+"-r")); err == nil && attempt > 0 {
			return attempt
		}
	}
	return 1
}

func dockerContainerName(item container.Summary) string {
	if len(item.Names) == 0 {
		return item.ID
	}
	return strings.TrimPrefix(item.Names[0], "/")
}
