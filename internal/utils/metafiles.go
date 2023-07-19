package utils

import "io/ioutil"

func GetArtifactFromFile(dir string) (string, error) {

	file, err := ioutil.ReadFile(dir + "/.artifact")
	if err != nil {
		return "", err
	}
	return string(file), nil
}
