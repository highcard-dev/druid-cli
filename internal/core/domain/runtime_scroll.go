package domain

import "time"

type RuntimeScrollStatus string

const (
	RuntimeScrollStatusCreated RuntimeScrollStatus = "created"
	RuntimeScrollStatusRunning RuntimeScrollStatus = "running"
	RuntimeScrollStatusStopped RuntimeScrollStatus = "stopped"
	RuntimeScrollStatusError   RuntimeScrollStatus = "error"
	RuntimeScrollStatusDeleted RuntimeScrollStatus = "deleted"
)

type RuntimeScroll struct {
	ID         string                `json:"id"`
	OwnerID    string                `json:"owner_id,omitempty"`
	Artifact   string                `json:"artifact"`
	ScrollRoot string                `json:"scroll_root"`
	DataRoot   string                `json:"data_root"`
	ScrollName string                `json:"scroll_name"`
	ScrollYAML string                `json:"-"`
	Status     RuntimeScrollStatus   `json:"status"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
	Commands   map[string]LockStatus `json:"commands,omitempty"`
}

type RuntimeState struct {
	Scrolls map[string]*RuntimeScroll `json:"scrolls"`
}
