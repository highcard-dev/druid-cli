package domain

// CommandIdentity is an opt-in permission request, never a credential.
type CommandIdentity struct {
	Permissions []string `yaml:"permissions" json:"permissions"`
}

// RuntimeCommandIdentity is transient mount metadata, never serialized in a Scroll.
type RuntimeCommandIdentity struct {
	ExecutionID string
	SecretName  string
	TokenPath   string
}

func ValidCommandPermission(permission string) bool {
	switch permission {
	case "commands:list", "commands:run", "commands:create", "queue:read", "logs:read", "console:write", "runtime:read", "ui:publish":
		return true
	}
	return false
}
