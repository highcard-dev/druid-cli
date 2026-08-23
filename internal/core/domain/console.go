package domain

type ConsoleType string

const (
	ConsoleTypeTTY       ConsoleType = "tty"
	ConsoleTypeContainer ConsoleType = "container"
)

type Console struct {
	Type ConsoleType `json:"type" validate:"required"`

	InputMode string `json:"inputMode" validate:"required"`

	Exit *int `json:"exit,omitempty"`
} //@name Console
