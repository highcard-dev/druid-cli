package kubernetes

import (
	"context"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

func (b *Backend) keepAliveTrafficIdleStopper(namespace string, root string, commandName string, procedureName string, procedure *domain.Procedure, globalPorts []domain.Port) jobIdleStopFunc {
	if !b.config.HubbleEnabled() || b.hubble == nil || procedure == nil || coldstarterProcedure(procedureName, procedure) {
		return nil
	}
	ports := portsByName(globalPorts)
	thresholds := make([]keepAliveThreshold, 0, len(procedure.ExpectedPorts))
	for _, expectedPort := range procedure.ExpectedPorts {
		if expectedPort.KeepAliveTraffic == "" {
			continue
		}
		threshold, err := domain.ParseKeepAliveTraffic(expectedPort.KeepAliveTraffic)
		if err != nil {
			return func(context.Context, *batchv1.Job) (bool, error) {
				return false, err
			}
		}
		port, ok := ports[expectedPort.Name]
		if !ok {
			return nil
		}
		thresholds = append(thresholds, keepAliveThreshold{
			expectedPort: expectedPort,
			port:         port,
			window:       threshold.Window,
		})
	}
	if len(thresholds) == 0 {
		return nil
	}
	_, scrollID, err := parseRef(root)
	if err != nil {
		return func(context.Context, *batchv1.Job) (bool, error) {
			return false, err
		}
	}
	return func(ctx context.Context, job *batchv1.Job) (bool, error) {
		if job == nil {
			return false, nil
		}
		now := time.Now()
		for _, threshold := range thresholds {
			if !keepAliveWindowElapsed(now, job.CreationTimestamp, threshold.window) {
				return false, nil
			}
		}
		for _, threshold := range thresholds {
			hasTraffic, err := b.hubble.HasFlow(ctx, TrafficQuery{
				Namespace:     namespace,
				ScrollID:      scrollID,
				ProcedureName: procedureName,
				Port:          threshold.port,
				ExpectedPort:  threshold.expectedPort,
				Window:        threshold.window,
			})
			if err != nil {
				logger.Log().Warn("Hubble Relay unavailable; keepAliveTraffic enforcement skipped",
					zap.String("namespace", namespace),
					zap.String("job", job.Name),
					zap.String("command", commandName),
					zap.String("procedure", procedureName),
					zap.String("port", threshold.expectedPort.Name),
					zap.Error(err),
				)
				return false, nil
			}
			if hasTraffic {
				return false, nil
			}
		}
		logger.Log().Info("Stopping idle Kubernetes procedure after keepAliveTraffic miss",
			zap.String("namespace", namespace),
			zap.String("job", job.Name),
			zap.String("command", commandName),
			zap.String("procedure", procedureName),
			zap.Int("ports", len(thresholds)),
		)
		if err := b.deleteJobAndWait(ctx, namespace, job.Name); err != nil {
			return false, err
		}
		return true, nil
	}
}

type keepAliveThreshold struct {
	expectedPort domain.ExpectedPort
	port         domain.Port
	window       time.Duration
}

func keepAliveWindowElapsed(now time.Time, created metav1.Time, window time.Duration) bool {
	if created.IsZero() {
		return false
	}
	return now.Sub(created.Time) >= window
}

func coldstarterProcedure(procedureName string, procedure *domain.Procedure) bool {
	if strings.EqualFold(procedureName, "coldstart") {
		return true
	}
	if procedure == nil {
		return false
	}
	for _, part := range procedure.Command {
		if strings.Contains(part, "druid-coldstarter") {
			return true
		}
	}
	return false
}
