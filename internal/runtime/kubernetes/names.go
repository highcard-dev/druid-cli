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
	labelProcedure = "druid.gg/procedure"
	labelPortName  = "druid.gg/port-name"
	labelCommand   = "druid.gg/command"
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

func shortHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:10]
}

func dataPVCName(id string) string {
	return dnsLabel("druid-" + id + "-data")
}

func stagingPVCName(artifact string) string {
	return dnsLabel("druid-stage-" + shortHash(artifact))
}

func jobName(prefix string, dataRoot string, procedureName string) string {
	return dnsLabel(fmt.Sprintf("druid-%s-%s-%s", prefix, refPVCName(dataRoot), procedureName))
}

func statefulSetName(dataRoot string, procedureName string) string {
	return dnsLabel(fmt.Sprintf("druid-sts-%s-%s", refPVCName(dataRoot), procedureName))
}

func serviceName(dataRoot string, procedureName string, portName string) string {
	return dnsLabel(fmt.Sprintf("druid-%s-%s-%s", refPVCName(dataRoot), procedureName, portName))
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
	return path.Join("data", mountSubPath)
}

func baseLabels(scrollID string) map[string]string {
	return map[string]string{
		labelManagedBy: "druid",
		labelComponent: "runtime",
		labelScrollID:  dnsLabel(scrollID),
	}
}
