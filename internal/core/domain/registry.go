package domain

type RegistryCredential struct {
	Host     string `json:"host" mapstructure:"host" yaml:"host"`
	Username string `json:"username" mapstructure:"username" yaml:"username"`
	Password string `json:"password" mapstructure:"password" yaml:"password"`
}
