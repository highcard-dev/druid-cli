package cli

import (
	"context"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	runtimehandlers "github.com/highcard-dev/daemon/apps/druid/adapters/http/handlers"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/devapi"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/net/webdav"
)

const (
	statusDisabled = "disabled"
	statusStarting = "starting"
	statusReady    = "ready"
	statusError    = "error"
)

type options struct {
	root           string
	listen         string
	runtimeID      string
	ownerID        string
	authJWKSURL    string
	runtimeJWKSURL string
}

func NewRootCommand() *cobra.Command {
	opt := options{}
	cmd := &cobra.Command{
		Use:   "druid-dev",
		Short: "Run the standalone Druid development file and build server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), opt)
		},
	}
	cmd.Flags().StringVar(&opt.root, "root", "", "Mounted runtime root")
	cmd.Flags().StringVar(&opt.listen, "listen", ":8084", "Dev server listen address")
	cmd.Flags().StringVar(&opt.runtimeID, "runtime-id", "", "Runtime id")
	cmd.Flags().StringVar(&opt.ownerID, "owner-id", "", "Runtime owner id")
	cmd.Flags().StringVar(&opt.authJWKSURL, "auth-jwks-url", "", "JWKS URL for customer JWTs")
	cmd.Flags().StringVar(&opt.runtimeJWKSURL, "runtime-jwks-url", "", "JWKS URL for short-lived runtime tokens")
	_ = cmd.MarkFlagRequired("root")
	return cmd
}

func run(ctx context.Context, opt options) error {
	root, err := filepath.Abs(opt.root)
	if err != nil {
		return err
	}
	auth := devAuth{runtimeID: opt.runtimeID, ownerID: opt.ownerID}
	if opt.authJWKSURL != "" {
		auth.user, err = coreservices.NewAuthorizer([]string{opt.authJWKSURL}, "")
		if err != nil {
			return err
		}
	}
	if opt.runtimeJWKSURL != "" {
		auth.runtime, err = coreservices.NewRuntimeTokenVerifier(opt.runtimeJWKSURL)
		if err != nil {
			return err
		}
	}
	server := newDevServer(root, auth)
	app := newApp(server)
	logger.Log().Info("Starting Druid development server",
		zap.String("root", root),
		zap.String("listen", opt.listen),
		zap.String("runtime_id", opt.runtimeID),
		zap.Bool("user_auth_enabled", auth.user != nil),
		zap.Bool("runtime_auth_enabled", auth.runtime != nil),
	)
	go func() {
		<-ctx.Done()
		logger.Log().Info("Stopping Druid development server")
		_ = server.stopWatch()
		_ = app.Shutdown()
	}()
	err = app.Listen(opt.listen)
	if err != nil {
		logger.Log().Error("Druid development server stopped with an error", zap.Error(err))
	} else {
		logger.Log().Info("Druid development server stopped")
	}
	return err
}

type devAuth struct {
	user      ports.AuthorizerServiceInterface
	runtime   ports.AuthorizerServiceInterface
	runtimeID string
	ownerID   string
}

type noopQueue struct{}

func (noopQueue) AddTempItem(string) error                     { return nil }
func (noopQueue) AddTempItemWithWait(string) error             { return nil }
func (noopQueue) GetQueue() map[string]domain.ScrollLockStatus { return nil }

type noopScroll struct{}

func (noopScroll) GetCommand(string) (*domain.CommandInstructionSet, error) {
	return nil, fmt.Errorf("watcher does not run daemon commands")
}
func (noopScroll) GetCurrent() *domain.Scroll { return nil }
func (noopScroll) GetFile() *domain.File      { return &domain.File{} }
func (noopScroll) GetDir() string             { return "" }
func (noopScroll) GetCwd() string             { return "" }

type watchConfig struct {
	paths   string
	workdir string
	command string
}

type devServer struct {
	root  string
	auth  devAuth
	watch ports.WatchServiceInterface

	mu      sync.RWMutex
	status  string
	lastErr string
	config  watchConfig
	cancel  context.CancelFunc
	watchID uint64
}

func newDevServer(root string, auth devAuth) *devServer {
	return &devServer{root: root, auth: auth, watch: coreservices.NewDevService(noopQueue{}, noopScroll{}), status: statusDisabled}
}

func newApp(server *devServer) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true, RequestMethods: append(fiber.DefaultMethods, "PROPFIND", "MKCOL", "MOVE", "COPY"), ErrorHandler: runtimehandlers.ErrorHandler})
	app.Use(runtimehandlers.RequestLogger)
	app.Use(func(c *fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", "*")
		c.Set("Access-Control-Allow-Methods", "GET,HEAD,PUT,POST,OPTIONS,PROPFIND,MKCOL,MOVE,COPY,DELETE")
		c.Set("Access-Control-Allow-Headers", "Origin,Content-Type,Accept,Authorization,Cache-Control,Depth,Destination,Overwrite")
		if c.Method() == fiber.MethodOptions && c.Path() != "/api/v1/files" && !strings.HasPrefix(c.Path(), "/webdav/") {
			return c.SendStatus(fiber.StatusNoContent)
		}
		return c.Next()
	})
	app.Use(server.authMiddleware)
	devapi.RegisterHandlers(app, server)
	webdavHandler := adaptor.HTTPHandler(&webdav.Handler{Prefix: "/webdav", FileSystem: webdav.Dir(server.root), LockSystem: webdav.NewMemLS()})
	app.All("/webdav/*", func(c *fiber.Ctx) error { return webdavHandler(c) })
	return app
}

func (s *devServer) GetHealth(c *fiber.Ctx) error { return c.SendString("ok") }

func (s *devServer) authMiddleware(c *fiber.Ctx) error {
	if c.Path() == "/health" || c.Method() == fiber.MethodOptions {
		return c.Next()
	}
	if s.auth.user == nil && s.auth.runtime == nil {
		return c.Next()
	}
	write := c.Method() == fiber.MethodPut || c.Method() == fiber.MethodPost || c.Method() == fiber.MethodPatch || c.Method() == fiber.MethodDelete || c.Method() == "MKCOL" || c.Method() == "MOVE" || c.Method() == "COPY"
	if s.auth.user != nil {
		if ctx, err := s.auth.user.CheckHeader(c); err == nil && ctx != nil {
			if s.auth.ownerID != "" && ctx.Subject != s.auth.ownerID {
				return fiber.NewError(fiber.StatusForbidden, "runtime owner mismatch")
			}
			return c.Next()
		} else if write {
			if err != nil {
				return fiber.NewError(fiber.StatusUnauthorized, err.Error())
			}
			return fiber.NewError(fiber.StatusUnauthorized, "missing token")
		}
	}
	if !write && s.auth.runtime != nil {
		if _, err := s.auth.runtime.CheckQuery(s.auth.runtimeID, c.Query("token")); err == nil {
			return c.Next()
		}
	}
	return fiber.NewError(fiber.StatusUnauthorized, "missing or invalid token")
}

func (s *devServer) GetFile(c *fiber.Ctx, params devapi.GetFileParams) error {
	return s.sendFile(c, params.Path)
}
func (s *devServer) HeadFile(c *fiber.Ctx, params devapi.HeadFileParams) error {
	return s.sendFile(c, params.Path)
}
func (s *devServer) OptionsFile(c *fiber.Ctx, _ devapi.OptionsFileParams) error {
	c.Set("DAV", "1")
	c.Set("Allow", "OPTIONS, GET, HEAD, PUT")
	return c.SendStatus(fiber.StatusNoContent)
}
func (s *devServer) PutFile(c *fiber.Ctx, params devapi.PutFileParams) error {
	return s.writeFile(c, params.Path)
}

func (s *devServer) GetWatchStatus(c *fiber.Ctx) error { return c.JSON(s.watchResponse()) }
func (s *devServer) DisableWatch(c *fiber.Ctx) error {
	_ = s.stopWatch()
	return c.JSON(s.watchResponse())
}

func (s *devServer) EnableWatch(c *fiber.Ctx) error {
	var request devapi.WatchModeRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if len(request.Command) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "command is required")
	}
	if len(request.WatchPaths) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "watch_paths is required")
	}
	workdir, err := s.directoryPath(request.WorkingDirectory)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	paths := make([]string, 0, len(request.WatchPaths))
	for _, path := range request.WatchPaths {
		if _, err := s.directoryPath(path); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		paths = append(paths, path)
	}
	config := watchConfig{paths: strings.Join(paths, "\x00"), workdir: request.WorkingDirectory, command: strings.Join(request.Command, "\x00")}
	s.mu.RLock()
	same := s.status == statusReady && s.config == config
	s.mu.RUnlock()
	if same {
		logger.Log().Info("Druid development watcher is already running with the requested configuration",
			zap.Strings("watch_paths", request.WatchPaths),
			zap.String("working_directory", request.WorkingDirectory),
			zap.Strings("command", request.Command),
		)
		return c.JSON(s.watchResponse())
	}
	_ = s.stopWatch()
	logger.Log().Info("Starting Druid development watcher",
		zap.Strings("watch_paths", request.WatchPaths),
		zap.String("working_directory", request.WorkingDirectory),
		zap.Strings("command", request.Command),
	)
	s.mu.Lock()
	s.status = statusStarting
	s.lastErr = ""
	s.config = config
	s.watchID++
	watchID := s.watchID
	s.mu.Unlock()
	if err := s.watch.StartWatching(s.root, paths...); err != nil {
		s.setWatchError(err)
		logger.Log().Error("Failed to start Druid development watcher", zap.Error(err))
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	ctx, cancel := context.WithCancel(context.Background())
	child := exec.CommandContext(ctx, request.Command[0], request.Command[1:]...)
	child.Dir = workdir
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		cancel()
		_ = s.watch.StopWatching()
		s.setWatchError(err)
		logger.Log().Error("Failed to start Druid development build process", zap.Error(err))
		return err
	}
	logger.Log().Info("Druid development build process started",
		zap.Int("pid", child.Process.Pid),
		zap.String("working_directory", workdir),
		zap.Strings("command", request.Command),
	)
	s.mu.Lock()
	s.cancel = cancel
	s.status = statusReady
	s.mu.Unlock()
	go func() {
		err := child.Wait()
		s.mu.Lock()
		if s.watchID == watchID {
			s.cancel = nil
			if err != nil && ctx.Err() == nil {
				s.status = statusError
				s.lastErr = err.Error()
			} else {
				s.status = statusDisabled
				s.lastErr = ""
			}
		}
		s.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			logger.Log().Error("Druid development build process exited with an error", zap.Error(err))
		} else {
			logger.Log().Info("Druid development build process stopped")
		}
		if ctx.Err() == nil {
			_ = s.watch.StopWatching()
		}
	}()
	return c.JSON(s.watchResponse())
}

func (s *devServer) WatchNotifications(c *fiber.Ctx) error {
	return websocket.New(func(conn *websocket.Conn) {
		defer conn.Close()
		sub := s.watch.Subscribe()
		if sub == nil {
			logger.Log().Info("Druid development reload client connected while watching is disabled")
			return
		}
		logger.Log().Info("Druid development reload client connected")
		defer logger.Log().Info("Druid development reload client disconnected")
		defer s.watch.Unsubscribe(sub)
		ping := time.NewTicker(30 * time.Second)
		defer ping.Stop()
		for {
			select {
			case msg, ok := <-sub:
				if !ok || msg == nil {
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, *msg); err != nil {
					logger.Log().Debug("Druid development reload client write failed", zap.Error(err))
					return
				}
			case <-ping.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	})(c)
}

func (s *devServer) stopWatch() error {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.watchID++
	s.status = statusDisabled
	s.lastErr = ""
	s.mu.Unlock()
	if cancel != nil || s.watch.IsWatching() {
		logger.Log().Info("Stopping Druid development watcher")
	}
	if cancel != nil {
		cancel()
	}
	return s.watch.StopWatching()
}
func (s *devServer) setWatchError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = statusError
	s.lastErr = err.Error()
	logger.Log().Error("Druid development watcher entered an error state", zap.Error(err))
}
func (s *devServer) watchResponse() devapi.WatchModeResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	response := devapi.WatchModeResponse{Status: devapi.WatchModeResponseStatus(s.status)}
	if s.lastErr != "" {
		response.Error = &s.lastErr
	}
	return response
}

func (s *devServer) sendFile(c *fiber.Ctx, raw string) error {
	full, err := s.filePath(raw)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return err
	}
	if contentType := mime.TypeByExtension(filepath.Ext(full)); contentType != "" {
		c.Set(fiber.HeaderContentType, contentType)
	}
	c.Set(fiber.HeaderContentLength, strconv.Itoa(len(data)))
	if c.Method() == fiber.MethodHead {
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Send(data)
}
func (s *devServer) writeFile(c *fiber.Ctx, raw string) error {
	full, err := s.filePath(raw)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(full, c.Body(), 0644); err != nil {
		return err
	}
	logger.Log().Info("Druid development file written", zap.String("path", raw), zap.Int("bytes", len(c.Body())))
	return c.SendStatus(fiber.StatusNoContent)
}
func (s *devServer) filePath(raw string) (string, error)      { return safePath(s.root, raw, false) }
func (s *devServer) directoryPath(raw string) (string, error) { return safePath(s.root, raw, true) }
func safePath(root, raw string, allowRoot bool) (string, error) {
	cleaned := filepath.Clean(strings.TrimPrefix(raw, "/"))
	if (cleaned == "." && !allowRoot) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid path %q", raw)
	}
	full := filepath.Join(root, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("invalid path %q", raw)
	}
	return full, nil
}
