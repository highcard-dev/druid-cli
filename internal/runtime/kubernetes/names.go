package kubernetes

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelComponent = "app.kubernetes.io/component"
	labelScrollID  = "druid.gg/scroll-id"
	labelRuntimeID = "druid.gg/runtime-id"
	labelProcedure = "druid.gg/procedure"
	labelPortName  = "druid.gg/port-name"
	labelCommand   = "druid.gg/command"
	labelAttempt   = "druid.gg/attempt"
)

var dnsLabelRe = regexp.MustCompile(`[^a-z0-9-]+`)

func dnsLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = dnsLabelRe.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		value = "scroll"
	}
	if len(value) <= 50 {
		return value
	}
	hash := shortHash(value)
	return strings.Trim(value[:40], "-") + "-" + hash
}

func objectName(value string) string {
	name := dnsLabel(value)
	// Services are stricter than most workload names: they must start with a letter.
	if name[0] < 'a' || name[0] > 'z' {
		name = "d-" + name
	}
	return name
}

func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:10]
}

func dataPVCName(id string) string {
	return dnsLabel("druid-" + id + "-data")
}

func runtimeID(root string) string {
	pvc := refPVCName(root)
	if strings.HasPrefix(pvc, "druid-") && strings.HasSuffix(pvc, "-data") {
		return strings.TrimSuffix(strings.TrimPrefix(pvc, "druid-"), "-data")
	}
	return pvc
}

func procedureResourceName(root string, commandName string, procedureIndex int) string {
	return objectName(fmt.Sprintf("%s-%s-%d", runtimeID(root), commandName, procedureIndex))
}

func jobName(prefix string, root string, procedureName string) string {
	return objectName(fmt.Sprintf("%s-%s-%s", runtimeID(root), prefix, procedureName))
}

func devStatefulSetName(root string) string {
	return dnsLabel(fmt.Sprintf("druid-dev-%s", refPVCName(root)))
}

func serviceName(root string, procedureName string, portName string) string {
	return objectName(fmt.Sprintf("%s-%s-%s", runtimeID(root), procedureName, portName))
}

func ref(namespace string, pvc string) string {
	return fmt.Sprintf("k8s://%s/%s", namespace, pvc)
}

func parseRef(value string) (string, string, error) {
	if !strings.HasPrefix(value, "k8s://") {
		return "", "", fmt.Errorf("kubernetes backend requires k8s://namespace/pvc refs, got %q", value)
	}
	trimmed := strings.TrimPrefix(value, "k8s://")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid kubernetes ref %q", value)
	}
	return parts[0], parts[1], nil
}

func refPVCName(value string) string {
	_, pvc, err := parseRef(value)
	if err != nil {
		return dnsLabel(value)
	}
	return pvc
}

func mountSubPath(mountSubPath string) string {
	if mountSubPath == "" {
		return "data"
	}
	clean := path.Clean(strings.TrimPrefix(mountSubPath, "/"))
	if clean == "." || clean == "data" || strings.HasPrefix(clean, "data/") {
		return clean
	}
	return path.Join("data", clean)
}

func baseLabels(scrollID string) map[string]string {
	return map[string]string{
		labelManagedBy: "druid",
		labelComponent: "runtime",
		labelScrollID:  dnsLabel(scrollID),
	}
}
