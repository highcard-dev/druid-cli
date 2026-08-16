package kubernetes

import "regexp"

var (
	uiPublishCommandPattern = regexp.MustCompile(`^ui_publish_(private|public)_[0-9a-f]{12}$`)
	uiPublishLabelPattern   = regexp.MustCompile(`^ui-publish-(private|public)-[0-9a-f]{12}$`)
)

func isUIPublishCommand(command string) bool {
	return uiPublishCommandPattern.MatchString(command)
}

func isUIPublishWorkload(command string, procedure string) bool {
	command = uiPublishCommandLabel(command)
	return command != "" && procedure == command+"-0"
}

func uiPublishCommandLabel(command string) string {
	if !uiPublishLabelPattern.MatchString(command) {
		return ""
	}
	return command
}
