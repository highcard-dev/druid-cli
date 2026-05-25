package kubernetes

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) RunCommand(command ports.RuntimeCommand) (*int, error) {
	if command.Command == nil {
		err := fmt.Errorf("kubernetes command %s has no instruction set", command.Name)
		logger.Log().Error("Cannot run Kubernetes command", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.Error(err))
		return nil, err
	}
	logger.Log().Info("Running Kubernetes command",
		zap.String("scroll_id", command.ScrollID),
		zap.String("command", command.Name),
		zap.String("run_mode", string(command.Command.Run)),
		zap.String("root", command.Root),
		zap.Int("procedures", len(command.Command.Procedures)),
	)
	portUse := expectedPortUse(command.Command)
	for idx, procedure := range command.Command.Procedures {
		if procedure == nil {
			logger.Log().Warn("Skipping nil Kubernetes procedure", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.Int("procedure_index", idx))
			continue
		}
		procedureName := domain.ProcedureName(command.Name, idx, procedure)
		resourceName := procedureResourceName(command.Root, command.Name, idx)
		env := command.ProcedureEnv[procedureName]
		if env == nil {
			env = procedure.Env
		}
		logger.Log().Debug("Kubernetes procedure selected",
			zap.String("scroll_id", command.ScrollID),
			zap.String("command", command.Name),
			zap.String("procedure", procedureName),
			zap.String("resource", resourceName),
			zap.String("run_mode", string(command.Command.Run)),
			zap.String("image", procedure.Image),
			zap.Bool("persistent", command.Command.Run == domain.RunModePersistent),
			zap.Bool("signal", procedure.IsSignal()),
			zap.Bool("ignore_failure", procedure.IgnoreFailure),
			zap.Int("env_count", len(env)),
			zap.Int("expected_ports", len(procedure.ExpectedPorts)),
			zap.Int("mounts", len(procedure.Mounts)),
		)
		logger.Log().Info("Starting Kubernetes procedure",
			zap.String("scroll_id", command.ScrollID),
			zap.String("command", command.Name),
			zap.String("procedure", procedureName),
			zap.String("resource", resourceName),
			zap.String("run_mode", string(command.Command.Run)),
			zap.String("image", procedure.Image),
			zap.Bool("signal", procedure.IsSignal()),
			zap.Int("expected_ports", len(procedure.ExpectedPorts)),
		)
		if command.Command.Run == domain.RunModePersistent {
			if procedure.IsSignal() {
				if err := b.Signal(procedureName, procedure.Target, procedure.Signal, command.Root); err != nil {
					logger.Log().Error("Kubernetes signal procedure failed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.String("target", procedure.Target), zap.String("signal", procedure.Signal), zap.Error(err))
					return nil, err
				}
				logger.Log().Info("Kubernetes signal procedure completed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.String("target", procedure.Target), zap.String("signal", procedure.Signal))
				continue
			}
			if procedure.Image == "" {
				err := fmt.Errorf("kubernetes procedure %s requires image", procedureName)
				logger.Log().Error("Kubernetes persistent procedure missing image", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Error(err))
				return nil, err
			}
			if err := b.ensurePersistentProcedure(context.Background(), command.ScrollID, command.Root, command.Name, procedureName, resourceName, procedure, command.GlobalPorts, env, portUse); err != nil {
				logger.Log().Error("Kubernetes persistent procedure failed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Error(err))
				return nil, err
			}
			continue
		}
		exitCode, err := b.runJobProcedure(command.ScrollID, command.Name, procedureName, resourceName, procedure, command.Root, command.GlobalPorts, env, portUse)
		if err != nil {
			if exitCode != nil && *exitCode != 0 && procedure.IgnoreFailure {
				logger.Log().Warn("Kubernetes job procedure failed but failure is ignored", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode), zap.Error(err))
				continue
			}
			logger.Log().Error("Kubernetes job procedure failed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Any("exit_code", exitCode), zap.Error(err))
			return exitCode, err
		}
		if exitCode != nil && *exitCode != 0 {
			if procedure.IgnoreFailure {
				logger.Log().Warn("Kubernetes job procedure failed but failure is ignored", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
				continue
			}
			logger.Log().Warn("Kubernetes command stopped after non-zero procedure exit", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
			return exitCode, nil
		}
		if exitCode != nil {
			logger.Log().Info("Kubernetes job procedure completed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
		}
	}
	logger.Log().Info("Kubernetes command completed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name))
	return nil, nil
}

func (b *Backend) runJobProcedure(scrollID string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, root string, globalPorts []domain.Port, env map[string]string, portUse map[string]int) (*int, error) {
	if procedure.IsSignal() {
		logger.Log().Info("Running Kubernetes signal procedure", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("target", procedure.Target), zap.String("signal", procedure.Signal))
		if err := b.Signal(procedureName, procedure.Target, procedure.Signal, root); err != nil {
			logger.Log().Error("Kubernetes signal procedure failed", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
			return nil, err
		}
		return nil, nil
	}
	if procedure.Image == "" {
		err := fmt.Errorf("kubernetes procedure %s requires image", procedureName)
		logger.Log().Error("Kubernetes job procedure missing image", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
		return nil, err
	}
	ctx := context.Background()
	if err := b.ensureExpectedServices(ctx, root, commandName, procedureName, procedure, globalPorts, portUse); err != nil {
		logger.Log().Error("Failed to reconcile Kubernetes procedure Services", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
		return nil, err
	}
	namespace, _, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Kubernetes job procedure root ref invalid", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("root", root), zap.Error(err))
		return nil, err
	}
	logger.Log().Info("Starting Kubernetes job procedure",
		zap.String("scroll_id", scrollID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.String("namespace", namespace),
		zap.String("base_job", resourceName),
	)
	logger.Log().Debug("Kubernetes job procedure details",
		zap.String("scroll_id", scrollID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.String("resource", resourceName),
		zap.String("image", procedure.Image),
		zap.Int("env_count", len(env)),
		zap.Int("expected_ports", len(procedure.ExpectedPorts)),
		zap.Int("mounts", len(procedure.Mounts)),
	)
	createdJob, err := b.createOrReuseProcedureJob(ctx, namespace, root, commandName, procedureName, resourceName, procedure, env)
	if err != nil {
		logger.Log().Error("Failed to create Kubernetes job procedure", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("base_job", resourceName), zap.Error(err))
		return nil, err
	}
	output := make(chan string, 100)
	consoleID := runtimeConsoleID(scrollID, procedureName)
	console, doneChan := b.consoleManager.AddConsoleWithChannel(consoleID, domain.ConsoleTypeContainer, "stdin", output)
	console.WriteInput = func(data string) error {
		return b.attachToProcedure(root, procedureName, data)
	}
	streamStarted := false
	jobName := createdJob.Name
	podName, err := b.waitForJobPod(ctx, namespace, jobName, string(createdJob.UID))
	if err == nil {
		streamStarted = true
		logger.Log().Debug("Streaming Kubernetes job procedure logs", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.String("pod", podName), zap.String("console_id", consoleID))
		go b.streamPodLogs(ctx, namespace, podName, output)
	} else {
		logger.Log().Warn("Could not find Kubernetes job pod before wait; console logs may be empty", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Error(err))
	}
	exitCode, err := b.waitForJob(ctx, namespace, jobName)
	if exitCode != nil {
		console.MarkExited(*exitCode)
	}
	if !streamStarted {
		close(output)
	}
	<-doneChan
	if err != nil {
		if exitCode != nil && *exitCode == 0 {
			b.deleteFinishedJob(context.Background(), namespace, jobName)
		}
		if exitCode != nil && *exitCode != 0 {
			logger.Log().Warn("Keeping failed Kubernetes job procedure for debugging", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
		}
		logger.Log().Error("Kubernetes job procedure ended with error", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Any("exit_code", exitCode), zap.Error(err))
		return exitCode, err
	}
	if exitCode != nil && *exitCode == 0 {
		b.deleteFinishedJob(context.Background(), namespace, jobName)
		logger.Log().Info("Kubernetes job procedure exited", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	} else if exitCode != nil {
		logger.Log().Warn("Keeping failed Kubernetes job procedure for debugging", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	}
	return exitCode, nil
}

func (b *Backend) ensurePersistentProcedure(ctx context.Context, scrollID string, root string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, globalPorts []domain.Port, env map[string]string, portUse map[string]int) error {
	if err := b.ensureExpectedServices(ctx, root, commandName, procedureName, procedure, globalPorts, portUse); err != nil {
		logger.Log().Error("Failed to reconcile Kubernetes persistent procedure Services", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.Error(err))
		return err
	}
	namespace, _, err := parseRef(root)
	if err != nil {
		logger.Log().Error("Kubernetes persistent procedure root ref invalid", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("root", root), zap.Error(err))
		return err
	}
	statefulSet, err := procedureStatefulSetSpec(namespace, root, commandName, procedureName, resourceName, procedure, env, b.config.RegistrySecret)
	if err != nil {
		logger.Log().Error("Failed to build Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.Error(err))
		return err
	}
	logger.Log().Info("Reconciling Kubernetes persistent procedure",
		zap.String("scroll_id", scrollID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.String("namespace", namespace),
		zap.String("statefulset", statefulSet.Name),
	)
	logger.Log().Debug("Kubernetes persistent procedure details",
		zap.String("scroll_id", scrollID),
		zap.String("command", commandName),
		zap.String("procedure", procedureName),
		zap.String("resource", resourceName),
		zap.String("image", procedure.Image),
		zap.Int("env_count", len(env)),
		zap.Int("expected_ports", len(procedure.ExpectedPorts)),
		zap.Int("mounts", len(procedure.Mounts)),
	)
	existing, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, statefulSet.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		logger.Log().Info("Creating Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name))
		if _, err := b.client.AppsV1().StatefulSets(namespace).Create(ctx, statefulSet, metav1.CreateOptions{}); err != nil {
			logger.Log().Error("Failed to create Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
			return err
		}
	case err != nil:
		logger.Log().Error("Failed to get Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
		return err
	default:
		logger.Log().Info("Updating Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.String("resource_version", existing.ResourceVersion))
		statefulSet.ResourceVersion = existing.ResourceVersion
		if _, err := b.client.AppsV1().StatefulSets(namespace).Update(ctx, statefulSet, metav1.UpdateOptions{}); err != nil {
			logger.Log().Error("Failed to update Kubernetes persistent procedure StatefulSet", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
			return err
		}
	}
	output := make(chan string, 100)
	console, _ := b.consoleManager.AddConsoleWithChannel(runtimeConsoleID(scrollID, procedureName), domain.ConsoleTypeContainer, "stdin", output)
	console.WriteInput = func(data string) error {
		return b.attachToProcedure(root, procedureName, data)
	}
	if err := b.waitForStatefulSet(ctx, namespace, statefulSet.Name); err != nil {
		close(output)
		logger.Log().Error("Kubernetes persistent procedure did not become ready", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
		return err
	}
	logger.Log().Info("Kubernetes persistent procedure ready", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name))
	go func() {
		podName, err := b.waitForPodBySelector(context.Background(), namespace, labels.SelectorFromSet(labels.Set{
			labelScrollID:  statefulSet.Labels[labelScrollID],
			labelProcedure: statefulSet.Labels[labelProcedure],
		}).String())
		if err != nil {
			logger.Log().Warn("Failed to find Kubernetes persistent procedure pod for logs", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
			output <- fmt.Sprintf("failed to find StatefulSet pod logs: %v", err)
			close(output)
			return
		}
		logger.Log().Debug("Streaming Kubernetes persistent procedure logs", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("pod", podName))
		b.streamPodLogs(context.Background(), namespace, podName, output)
	}()
	return nil
}
