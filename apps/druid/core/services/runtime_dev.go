package services

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type DevWatchRequest struct {
	WatchPaths        []string `json:"watchPaths"`
	HotReloadCommands []string `json:"hotReloadCommands,omitempty"`
}

type DevWatchStatus struct {
	Enabled      bool     `json:"enabled"`
	WatchedPaths []string `json:"watchedPaths"`
}

func (s *RuntimeSupervisor) AddCommand(id string, command string, instruction *domain.CommandInstructionSet) error {
	session, err := s.sessionFor(id)
	if err != nil {
		return err
	}
	return session.AddCommand(command, instruction)
}

func (s *RuntimeSupervisor) EnableDevWatch(id string, request DevWatchRequest) (DevWatchStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return DevWatchStatus{}, err
	}
	return session.EnableDevWatch(request)
}

func (s *RuntimeSupervisor) DisableDevWatch(id string) (DevWatchStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return DevWatchStatus{}, err
	}
	return session.DisableDevWatch()
}

func (s *RuntimeSupervisor) DevWatchStatus(id string) (DevWatchStatus, error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return DevWatchStatus{}, err
	}
	return session.DevWatchStatus(), nil
}

func (s *RuntimeSupervisor) SubscribeDevWatch(id string) (chan *[]byte, func(), error) {
	session, err := s.sessionFor(id)
	if err != nil {
		return nil, nil, err
	}
	ch := session.SubscribeDevWatch()
	if ch == nil {
		return nil, nil, fmt.Errorf("dev watch is not enabled")
	}
	return ch, func() { session.UnsubscribeDevWatch(ch) }, nil
}

func (s *RuntimeSession) AddCommand(command string, instruction *domain.CommandInstructionSet) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	if instruction == nil {
		return fmt.Errorf("command instruction is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.scrollService.GetFile()
	if file.Commands == nil {
		file.Commands = map[string]*domain.CommandInstructionSet{}
	}
	file.Commands[command] = instruction
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	s.runtimeScroll.ScrollYAML = string(data)
	return s.store.UpdateScroll(s.runtimeScroll)
}

func (s *RuntimeSession) EnableDevWatch(request DevWatchRequest) (DevWatchStatus, error) {
	if len(request.WatchPaths) == 0 {
		request.WatchPaths = []string{"."}
	}
	for _, command := range request.HotReloadCommands {
		if _, err := s.scrollService.GetCommand(command); err != nil {
			return DevWatchStatus{}, err
		}
	}
	s.mu.Lock()
	root := s.runtimeScroll.Root
	id := s.runtimeScroll.ID
	routing := append([]domain.RuntimeRouteAssignment(nil), s.runtimeScroll.Routing...)
	s.devWatchPaths = append([]string(nil), request.WatchPaths...)
	s.devCommands = append([]string(nil), request.HotReloadCommands...)
	s.mu.Unlock()

	if s.devDaemonURL == "" {
		return DevWatchStatus{}, fmt.Errorf("dev daemon URL is not configured")
	}
	if err := s.runtimeBackend.StartDev(context.Background(), ports.RuntimeDevAction{
		RuntimeID:         id,
		OwnerID:           s.runtimeScroll.OwnerID,
		RootRef:           root,
		MountPath:         "/scroll",
		Listen:            ":8084",
		WatchPaths:        request.WatchPaths,
		HotReloadCommands: request.HotReloadCommands,
		Routing:           routing,
		DaemonURL:         s.devDaemonURL,
		DaemonToken:       s.devDaemonToken,
		AuthJWKSURL:       s.devAuthJWKSURL,
		RuntimeJWKSURL:    s.devRuntimeJWKSURL,
	}); err != nil {
		return DevWatchStatus{}, err
	}
	s.startDevWatchBridge(routing)
	return s.DevWatchStatus(), nil
}

func (s *RuntimeSession) DisableDevWatch() (DevWatchStatus, error) {
	s.mu.Lock()
	root := s.runtimeScroll.Root
	s.devWatchPaths = nil
	s.devCommands = nil
	if s.devWatchCancel != nil {
		s.devWatchCancel()
		s.devWatchCancel = nil
	}
	if s.devWatchBridge != nil {
		s.devWatchBridge.Close()
		s.devWatchBridge = nil
	}
	s.mu.Unlock()
	if err := s.runtimeBackend.StopDev(context.Background(), root); err != nil {
		return DevWatchStatus{}, err
	}
	return s.DevWatchStatus(), nil
}

func (s *RuntimeSession) DevWatchStatus() DevWatchStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.devWatchPaths) == 0 {
		return DevWatchStatus{Enabled: false, WatchedPaths: []string{}}
	}
	return DevWatchStatus{Enabled: true, WatchedPaths: append([]string(nil), s.devWatchPaths...)}
}

func (s *RuntimeSession) SubscribeDevWatch() chan *[]byte {
	if s.watchService != nil {
		return s.watchService.Subscribe()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.devWatchBridge == nil {
		return nil
	}
	return s.devWatchBridge.Subscribe()
}

func (s *RuntimeSession) UnsubscribeDevWatch(ch chan *[]byte) {
	if s.watchService != nil {
		s.watchService.Unsubscribe(ch)
		return
	}
	s.mu.Lock()
	bridge := s.devWatchBridge
	s.mu.Unlock()
	if bridge != nil {
		bridge.Unsubscribe(ch)
	}
}

func (s *RuntimeSession) startDevWatchBridge(routing []domain.RuntimeRouteAssignment) {
	bridgeURL := devWatchBridgeURL(routing)
	if bridgeURL == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	bridge := domain.NewHub()
	s.mu.Lock()
	if s.devWatchCancel != nil {
		s.devWatchCancel()
	}
	if s.devWatchBridge != nil {
		s.devWatchBridge.Close()
	}
	s.devWatchCancel = cancel
	s.devWatchBridge = bridge
	runtimeID := s.runtimeScroll.ID
	s.mu.Unlock()
	go bridge.Run()
	go func() {
		defer bridge.Close()
		backoff := time.Second
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			conn, _, err := websocket.DefaultDialer.Dial(bridgeURL, nil)
			if err != nil {
				logger.Log().Warn("Failed to connect dev watch bridge", zap.String("runtime_id", runtimeID), zap.String("url", bridgeURL), zap.Error(err))
				if !sleepDevWatchBridge(ctx, backoff) {
					return
				}
				if backoff < 30*time.Second {
					backoff *= 2
				}
				continue
			}
			backoff = time.Second
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					_ = conn.Close()
					break
				}
				copyMsg := append([]byte(nil), msg...)
				bridge.Broadcast(copyMsg)
			}
		}
	}()
}

func sleepDevWatchBridge(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func devWatchBridgeURL(routing []domain.RuntimeRouteAssignment) string {
	for _, assignment := range routing {
		if assignment.PortName != "webdav" && assignment.Name != "webdav" {
			continue
		}
		base := strings.TrimRight(assignment.URL, "/")
		if base == "" {
			host := assignment.Host
			if host == "" {
				host = assignment.ExternalIP
			}
			if host == "" || assignment.PublicPort == 0 {
				continue
			}
			base = fmt.Sprintf("http://%s:%d", host, assignment.PublicPort)
		}
		parsed, err := url.Parse(base)
		if err != nil {
			continue
		}
		switch parsed.Scheme {
		case "https":
			parsed.Scheme = "wss"
		default:
			parsed.Scheme = "ws"
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/ws/v1/watch/notify"
		return parsed.String()
	}
	return ""
}
