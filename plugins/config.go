package plugin

import "gopkg.in/yaml.v2"

func GetConfig[T interface{}](pluginName string, scrollConfigRawYaml []byte) (T, error) {
	var Config T

	var scrollConfig map[string]interface{}

	yaml.Unmarshal(scrollConfigRawYaml, &scrollConfig)

	rcon := scrollConfig[pluginName]

	b, err := yaml.Marshal(rcon)

	if err != nil {
		return Config, err
	}

	yaml.Unmarshal(b, &Config)

	return Config, nil
}
