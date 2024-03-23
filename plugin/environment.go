package plugin

import (
	"fmt"
)

type Environment struct {
	Address  string
	Password string
}

func NewPluginEnvironment(cwd string, password string, port int, host string) (*Environment, error) {
	environment := &Environment{}
	if host == "" {
		host = "localhost"
	}

	environment.Address = fmt.Sprintf("%s:%d", host, port)
	environment.Password = password

	return environment, nil
}
