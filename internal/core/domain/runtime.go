package domain

const DefaultExecImage = "bash:latest"

const RuntimeDataDir = "data"
const RuntimeConfigDir = ".druid"
const RuntimeConfigFile = "runtime.json"

type RuntimeConfig struct {
	SchemaVersion string                `json:"schemaVersion"`
	Scroll        RuntimeConfigScroll   `json:"scroll"`
	Paths         RuntimeConfigPaths    `json:"paths"`
	Ports         []Port                `json:"ports"`
	ExpectedPorts []RuntimeExpectedPort `json:"expectedPorts,omitempty"`
	Runtime       RuntimeConfigRuntime  `json:"runtime"`
}

type RuntimeConfigScroll struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Artifact string `json:"artifact"`
}

type RuntimeConfigPaths struct {
	Data          string `json:"data"`
	RuntimeConfig string `json:"runtimeConfig"`
}

type RuntimeExpectedPort struct {
	Name             string `json:"name"`
	Procedure        string `json:"procedure"`
	Port             int    `json:"port"`
	Protocol         string `json:"protocol"`
	KeepAliveTraffic string `json:"keepAliveTraffic,omitempty"`
}

type RuntimeConfigRuntime struct {
	Backend     string `json:"backend"`
	GeneratedAt string `json:"generatedAt"`
}
