package docker

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

const dockerHostGatewayExtraHost = "host.docker.internal:host-gateway"

const (
	dockerLabelRole      = "druid.role"
	dockerLabelRuntimeID = "druid.runtime-id"
	dockerLabelRootHash  = "druid.root-hash"
	dockerLabelCommand   = "druid.command"
	dockerLabelProcedure = "druid.procedure"
	dockerLabelResource  = "druid.resource"
	dockerLabelAttempt   = "druid.attempt"
	dockerRoleProcedure  = "procedure"
)

const dockerFailedProcedureRetention = 3

func dockerExtraHosts() []string {
	if runtime.GOOS == "linux" {
		return []string{dockerHostGatewayExtraHost}
	}
	return nil
}

func ContainerName(root string, commandName string) string {
	return sanitizeContainerName(fmt.Sprintf("%s-%s", runtimeID(root), commandName))
}

func runtimeID(root string) string {
	switch {
	case strings.HasPrefix(root, "docker-volume://"):
		name := strings.TrimPrefix(root, "docker-volume://")
		if strings.HasPrefix(name, "druid-") && strings.HasSuffix(name, "-data") {
			return strings.TrimSuffix(strings.TrimPrefix(name, "druid-"), "-data")
		}
		return name
	case strings.HasPrefix(root, "docker-bind://"):
		return strings.TrimSuffix(filepath.Base(strings.TrimPrefix(root, "docker-bind://")), "-data")
	default:
		return strings.TrimSuffix(filepath.Base(root), "-data")
	}
}

func procedureResourceName(commandName string, procedureIndex int) string {
	return fmt.Sprintf("%s-%d", commandName, procedureIndex)
}

func rootHash(root string) string {
	hash := sha1.Sum([]byte(root))
	return hex.EncodeToString(hash[:])[:10]
}

func runtimeConsoleID(scrollID string, procedureName string) string {
	if scrollID == "" {
		return procedureName
	}
	return scrollID + "/" + procedureName
}

func sanitizeContainerName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
	name = re.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-_.")
	if name == "" {
		return "command"
	}
	return name
}

func dockerWorkerEnv(base []string) []string {
	if plainHTTP := os.Getenv("DRUID_REGISTRY_PLAIN_HTTP"); plainHTTP != "" {
		base = append(base, "DRUID_REGISTRY_PLAIN_HTTP="+plainHTTP)
	}
	return base
}

func dockerSetupError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "ports are not available") ||
		strings.Contains(message, "bind: address already in use") ||
		strings.Contains(message, "port is already allocated") {
		return domain.NonRetryableCommand(err)
	}
	return err
}
