package api

type DevWatchRequest struct {
	WatchPaths        []string `json:"watchPaths"`
	HotReloadCommands []string `json:"hotReloadCommands,omitempty"`
}

type DevWatchResponse struct {
	Status       string   `json:"status"`
	Enabled      bool     `json:"enabled"`
	WatchedPaths []string `json:"watchedPaths,omitempty"`
}

type DevWatchStatus struct {
	Enabled      bool     `json:"enabled"`
	WatchedPaths []string `json:"watchedPaths"`
}
