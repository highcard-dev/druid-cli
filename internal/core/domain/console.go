package domain

type Console struct {
	Channel *BroadcastChannel `json:"-" validate:"required"`

	Type string `json:"type" validate:"required"`

	InputMode string `json:"inputMode" validate:"required"`
} //@name Console
