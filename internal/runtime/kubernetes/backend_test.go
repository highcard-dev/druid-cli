package kubernetes

import (
	"testing"

	"k8s.io/client-go/rest"
)

func TestTuneRESTConfigSetsRateLimits(t *testing.T) {
	config := &rest.Config{}

	tuneRESTConfig(config)

	if config.QPS != defaultKubernetesClientQPS {
		t.Fatalf("QPS = %v, want %v", config.QPS, defaultKubernetesClientQPS)
	}
	if config.Burst != defaultKubernetesClientBurst {
		t.Fatalf("Burst = %v, want %v", config.Burst, defaultKubernetesClientBurst)
	}
}

func TestTuneRESTConfigKeepsExplicitRateLimits(t *testing.T) {
	config := &rest.Config{QPS: 7, Burst: 11}

	tuneRESTConfig(config)

	if config.QPS != 7 {
		t.Fatalf("QPS = %v, want 7", config.QPS)
	}
	if config.Burst != 11 {
		t.Fatalf("Burst = %v, want 11", config.Burst)
	}
}
