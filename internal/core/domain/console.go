package domain

type ConsoleType string

const (
	ConsoleTypeTTY     ConsoleType = "tty"
	ConsoleTypeProcess ConsoleType = "process"
	ConsoleTypePlugin  ConsoleType = "plugin"
)

type Console struct {
	Channel *BroadcastChannel `json:"-" validate:"required"`

	Type ConsoleType `json:"type" validate:"required"`

	InputMode string `json:"inputMode" validate:"required"`

	Exit *int `json:"exit,omitempty"`
} //@name Console

type StreamItem struct {
	Data   string
	Stream string
}

func (c *Console) MarkExited(exitCode int) {
	c.Exit = &exitCode
}
