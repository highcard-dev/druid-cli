package kubernetes

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	if command.Command.Run == domain.RunModePersistent {
		return nil, b.runPersistentCommand(command)
	}
	startIndex := 0
	if command.Command.Run == domain.RunModeRestart {
		resumeIndex, err := b.resumeRestartProcedureIndex(context.Background(), command.Root, command.Name, command.Command)
		if err != nil {
			logger.Log().Error("Failed to inspect active Kubernetes restart procedures", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.Error(err))
			return nil, err
		}
		startIndex = resumeIndex
	}
	for idx, procedure := range command.Command.Procedures {
		if procedure == nil {
			logger.Log().Warn("Skipping nil Kubernetes procedure", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.Int("procedure_index", idx))
			continue
		}
		procedureName := domain.ProcedureName(command.Name, idx, procedure)
		if idx < startIndex {
			logger.Log().Info("Skipping earlier Kubernetes restart procedure because a later procedure is already active",
				zap.String("scroll_id", command.ScrollID),
				zap.String("command", command.Name),
				zap.String("procedure", procedureName),
				zap.Int("procedure_index", idx),
				zap.Int("resume_index", startIndex),
			)
			continue
		}
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
		b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusRunning, nil)
		exitCode, err := b.runJobProcedure(command.ScrollID, command.Name, procedureName, resourceName, procedure, command.Root, command.Ports, env)
		if err != nil {
			if exitCode != nil && *exitCode != 0 && procedure.IgnoreFailure {
				b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusDone, exitCode)
				logger.Log().Warn("Kubernetes job procedure failed but failure is ignored", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode), zap.Error(err))
				continue
			}
			b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusError, exitCode)
			logger.Log().Error("Kubernetes job procedure failed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Any("exit_code", exitCode), zap.Error(err))
			return exitCode, err
		}
		if exitCode != nil && *exitCode != 0 {
			if procedure.IgnoreFailure {
				b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusDone, exitCode)
				logger.Log().Warn("Kubernetes job procedure failed but failure is ignored", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
				continue
			}
			b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusError, exitCode)
			logger.Log().Warn("Kubernetes command stopped after non-zero procedure exit", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
			return exitCode, nil
		}
		b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusDone, exitCode)
		if exitCode != nil {
			logger.Log().Info("Kubernetes job procedure completed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("procedure", procedureName), zap.Int("exit_code", *exitCode))
		}
	}
	logger.Log().Info("Kubernetes command completed", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name))
	return nil, nil
}

func (b *Backend) observeProcedureStatus(command ports.RuntimeCommand, procedure string, status domain.ScrollLockStatus, exitCode *int) {
	if b.procedureStatusObserver == nil {
		return
	}
	b.procedureStatusObserver.ObserveProcedureStatus(ports.ProcedureStatusUpdate{
		RuntimeID: command.ScrollID,
		Command:   command.Name,
		Procedure: procedure,
		Status:    status,
		ExitCode:  exitCode,
	})
}

func (b *Backend) runJobProcedure(scrollID string, commandName string, procedureName string, resourceName string, procedure *domain.Procedure, root string, ports []domain.Port, env map[string]string) (*int, error) {
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
	if err := b.ensureExpectedServices(ctx, root, commandName, procedureName, procedure, ports); err != nil {
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
	jobName := createdJob.Name
	exitCode, err := b.waitForJobWithIdleStop(ctx, namespace, jobName, b.keepAliveTrafficIdleStopper(namespace, root, commandName, procedureName, procedure, ports))
	if err != nil {
		if exitCode != nil && *exitCode != 0 {
			logger.Log().Warn("Keeping failed Kubernetes job procedure for debugging", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
		}
		logger.Log().Error("Kubernetes job procedure ended with error", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Any("exit_code", exitCode), zap.Error(err))
		return exitCode, err
	}
	if exitCode != nil && *exitCode == 0 {
		logger.Log().Info("Kubernetes job procedure exited", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	} else if exitCode != nil {
		logger.Log().Warn("Keeping failed Kubernetes job procedure for debugging", zap.String("scroll_id", scrollID), zap.String("command", commandName), zap.String("procedure", procedureName), zap.String("namespace", namespace), zap.String("job", jobName), zap.Int("exit_code", *exitCode))
	}
	return exitCode, nil
}

func (b *Backend) runPersistentCommand(command ports.RuntimeCommand) error {
	ctx := context.Background()
	last := -1
	for idx, procedure := range command.Command.Procedures {
		if procedure == nil {
			continue
		}
		procedureName := domain.ProcedureName(command.Name, idx, procedure)
		if procedure.IsSignal() || procedure.Image == "" {
			err := fmt.Errorf("kubernetes persistent procedure %s must be a container with an image", procedureName)
			b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusError, nil)
			return err
		}
		if err := b.ensurePersistentExpectedServices(ctx, command.Root, command.Name, procedureName, procedure, command.Ports); err != nil {
			b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusError, nil)
			return err
		}
		b.observeProcedureStatus(command, procedureName, domain.ScrollLockStatusRunning, nil)
		last = idx
	}
	if last < 0 {
		return fmt.Errorf("kubernetes persistent command %s requires at least one procedure", command.Name)
	}
	mainProcedure := command.Command.Procedures[last]
	mainName := domain.ProcedureName(command.Name, last, mainProcedure)
	resourceName := procedureResourceName(command.Root, command.Name, last)
	namespace, pvc, err := parseRef(command.Root)
	if err != nil {
		logger.Log().Error("Kubernetes persistent command root ref invalid", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("root", command.Root), zap.Error(err))
		return err
	}
	statefulSet, err := persistentStatefulSetSpec(namespace, command.Root, command.Name, resourceName, command.Command, command.ProcedureEnv, b.config.RegistrySecret)
	if err != nil {
		logger.Log().Error("Failed to build Kubernetes persistent command StatefulSet", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("namespace", namespace), zap.Error(err))
		return err
	}
	if err := b.pinPodToRuntimeNode(ctx, namespace, pvc, &statefulSet.Spec.Template.Spec); err != nil {
		return err
	}
	if err := b.deleteLegacyPersistentStatefulSets(ctx, namespace, pvc, command.Name, statefulSet.Name); err != nil {
		return err
	}
	logger.Log().Info("Reconciling Kubernetes persistent command",
		zap.String("scroll_id", command.ScrollID),
		zap.String("command", command.Name),
		zap.String("main_procedure", mainName),
		zap.String("namespace", namespace),
		zap.String("statefulset", statefulSet.Name),
		zap.Int("init_containers", len(statefulSet.Spec.Template.Spec.InitContainers)),
	)
	existing, err := b.client.AppsV1().StatefulSets(namespace).Get(ctx, statefulSet.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		logger.Log().Info("Creating Kubernetes persistent command StatefulSet", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name))
		if _, err := b.client.AppsV1().StatefulSets(namespace).Create(ctx, statefulSet, metav1.CreateOptions{}); err != nil {
			logger.Log().Error("Failed to create Kubernetes persistent command StatefulSet", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
			return err
		}
	case err != nil:
		logger.Log().Error("Failed to get Kubernetes persistent command StatefulSet", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
		return err
	default:
		logger.Log().Info("Updating Kubernetes persistent command StatefulSet", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.String("resource_version", existing.ResourceVersion))
		statefulSet.ResourceVersion = existing.ResourceVersion
		if _, err := b.client.AppsV1().StatefulSets(namespace).Update(ctx, statefulSet, metav1.UpdateOptions{}); err != nil {
			logger.Log().Error("Failed to update Kubernetes persistent command StatefulSet", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name), zap.Error(err))
			return err
		}
	}
	logger.Log().Info("Kubernetes persistent command started", zap.String("scroll_id", command.ScrollID), zap.String("command", command.Name), zap.String("namespace", namespace), zap.String("statefulset", statefulSet.Name))
	return nil
}

func (b *Backend) deleteLegacyPersistentStatefulSets(ctx context.Context, namespace string, pvc string, commandName string, keepName string) error {
	selector := fmt.Sprintf("%s=%s,%s=%s", labelScrollID, dnsLabel(pvc), labelCommand, dnsLabel(commandName))
	statefulSets, err := b.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, statefulSet := range statefulSets.Items {
		if statefulSet.Name == keepName {
			continue
		}
		if err := b.client.AppsV1().StatefulSets(namespace).Delete(ctx, statefulSet.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
