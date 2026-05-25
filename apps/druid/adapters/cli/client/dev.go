package client

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	runtimehandlers "github.com/highcard-dev/daemon/apps/druid/adapters/http/handlers"
	"github.com/highcard-dev/daemon/internal/api"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	coreservices "github.com/highcard-dev/daemon/internal/core/services"
	"github.com/highcard-dev/daemon/internal/devapi"
	"github.com/spf13/cobra"
	"golang.org/x/net/webdav"
)

var devWatchPaths []string
var devCommands []string
var devDisable bool
var devStatus bool
var devTrigger bool
var devRoot string
var devListen string
var devRuntimeID string
var devDaemonURL string
var devDaemonToken string
var devOwnerID string
var devAuthJWKSURL string
var devRuntimeJWKSURL string

var DevCommand = &cobra.Command{
	Use:   "dev [name]",
	Short: "Control daemon-backed scroll development mode",
	Example: `  druid dev my-scroll --watch private/dist
  druid dev my-scroll --watch private/dist --command build
	druid dev --root /scroll --listen :8084 --runtime-id my-scroll
	druid dev my-scroll --status
  druid dev my-scroll --disable`,
	Args: cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if devRoot != "" {
			return runDevServer()
		}
		if len(args) != 1 {
			return fmt.Errorf("scroll name is required unless --root is set")
		}
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		id := args[0]
		modes := 0
		for _, enabled := range []bool{devDisable, devStatus} {
			if enabled {
				modes++
			}
		}
		if modes > 1 || devTrigger {
			return fmt.Errorf("--status and --disable cannot be combined; use druid run <name> <command> to trigger commands")
		}
		if devStatus {
			status, err := daemon.WatchStatus(cmd.Context(), id)
			if err != nil {
				return err
			}
			return printJSON(status)
		}
		if devDisable {
			status, err := daemon.DisableWatch(cmd.Context(), id)
			if err != nil {
				return err
			}
			return printJSON(status)
		}
		if len(devWatchPaths) == 0 {
			devWatchPaths = []string{"."}
		}
		status, err := daemon.EnableWatch(cmd.Context(), id, api.DevWatchRequest{
			WatchPaths:        devWatchPaths,
			HotReloadCommands: devCommands,
		})
		if err != nil {
			return err
		}
		return printJSON(status)
	},
}

func init() {
	DevCommand.Flags().StringSliceVar(&devWatchPaths, "watch", nil, "Path to watch, relative to the scroll root; repeatable")
	DevCommand.Flags().StringSliceVar(&devCommands, "command", nil, "Scroll command to run on startup and file changes; repeatable")
	DevCommand.Flags().BoolVar(&devDisable, "disable", false, "Disable development watch mode")
	DevCommand.Flags().BoolVar(&devStatus, "status", false, "Show development watch mode status")
	DevCommand.Flags().BoolVar(&devTrigger, "trigger", false, "Deprecated; use druid run <name> <command>")
	DevCommand.Flags().StringVar(&devRoot, "root", "", "Mounted runtime root; when set, run the dev WebDAV/watch server")
	DevCommand.Flags().StringVar(&devListen, "listen", ":8084", "Dev server listen address")
	DevCommand.Flags().StringVar(&devRuntimeID, "runtime-id", "", "Runtime id")
	DevCommand.Flags().StringVar(&devDaemonURL, "daemon-url", "", "Daemon management API URL")
	DevCommand.Flags().StringVar(&devDaemonToken, "daemon-token", "", "Daemon management token")
	DevCommand.Flags().StringVar(&devOwnerID, "owner-id", "", "Runtime owner id for customer-facing auth")
	DevCommand.Flags().StringVar(&devAuthJWKSURL, "auth-jwks-url", "", "JWKS URL for customer JWTs")
	DevCommand.Flags().StringVar(&devRuntimeJWKSURL, "runtime-jwks-url", "", "JWKS URL for short-lived runtime tokens")
}

func runDevServer() error {
	if devRuntimeID == "" {
		return fmt.Errorf("--runtime-id is required with --root")
	}
	root, err := filepath.Abs(devRoot)
	if err != nil {
		return err
	}
	if len(devWatchPaths) == 0 {
		devWatchPaths = []string{"."}
	}
	if devDaemonURL == "" {
		devDaemonURL = os.Getenv("DRUID_DAEMON_URL")
	}
	if devDaemonToken == "" {
		devDaemonToken = os.Getenv("DRUID_INTERNAL_TOKEN")
	}
	auth := devAuth{runtimeID: devRuntimeID, ownerID: devOwnerID}
	if devAuthJWKSURL != "" {
		auth.user, err = coreservices.NewAuthorizer([]string{devAuthJWKSURL}, "")
		if err != nil {
			return err
		}
	}
	if devRuntimeJWKSURL != "" {
		auth.runtime, err = coreservices.NewRuntimeTokenVerifier(devRuntimeJWKSURL)
		if err != nil {
			return err
		}
	}
	broadcast := domain.NewHub()
	go broadcast.Run()
	queue := &devTriggerQueue{broadcast: broadcast, commands: append([]string(nil), devCommands...)}
	watch := coreservices.NewDevService(queue, devScrollService{commands: devCommands})
	if len(devCommands) > 0 {
		if err := watch.SetHotReloadCommands(devCommands); err != nil {
			return err
		}
	}
	if err := watch.StartWatching(root, devWatchPaths...); err != nil {
		return err
	}

	app := newDevApp(root, broadcast, queue, auth)
	return app.Listen(devListen)
}

type devAuth struct {
	user      ports.AuthorizerServiceInterface
	runtime   ports.AuthorizerServiceInterface
	runtimeID string
	ownerID   string
}

func newDevApp(root string, broadcast *domain.BroadcastChannel, queue *devTriggerQueue, authOpt ...devAuth) *fiber.App {
	auth := devAuth{}
	if len(authOpt) > 0 {
		auth = authOpt[0]
	}
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		RequestMethods:        append(fiber.DefaultMethods, "PROPFIND", "MKCOL", "MOVE", "COPY"),
		ErrorHandler:          runtimehandlers.ErrorHandler,
	})
	app.Use(runtimehandlers.RequestLogger)
	server := devServer{root: root, broadcast: broadcast, queue: queue, auth: auth}
	app.Use(func(c *fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", "*")
		c.Set("Access-Control-Allow-Methods", "GET,HEAD,PUT,OPTIONS,PROPFIND,MKCOL,MOVE,COPY,DELETE")
		c.Set("Access-Control-Allow-Headers", "Origin,Content-Type,Accept,Authorization,Cache-Control,Depth,Destination,Overwrite")
		if c.Method() == fiber.MethodOptions && c.Path() != "/api/v1/files" && !strings.HasPrefix(c.Path(), "/webdav/") {
			return c.SendStatus(fiber.StatusNoContent)
		}
		return c.Next()
	})
	app.Use(server.authMiddleware)
	devapi.RegisterHandlers(app, server)
	webdavHandler := adaptor.HTTPHandler(&webdav.Handler{
		Prefix:     "/webdav",
		FileSystem: webdav.Dir(root),
		LockSystem: webdav.NewMemLS(),
	})
	app.All("/webdav/*", func(c *fiber.Ctx) error {
		if err := webdavHandler(c); err != nil {
			return err
		}
		switch c.Method() {
		case fiber.MethodPut, "DELETE", "MKCOL", "MOVE", "COPY":
			if c.Response().StatusCode() < fiber.StatusBadRequest {
				server.queue.Trigger()
			}
		}
		return nil
	})
	return app
}

type devServer struct {
	root      string
	broadcast *domain.BroadcastChannel
	queue     *devTriggerQueue
	auth      devAuth
}

func (s devServer) GetHealth(c *fiber.Ctx) error { return c.SendString("ok") }

func (s devServer) authMiddleware(c *fiber.Ctx) error {
	if c.Path() == "/health" || c.Method() == fiber.MethodOptions {
		return c.Next()
	}
	if s.auth.user == nil && s.auth.runtime == nil {
		return c.Next()
	}
	write := c.Method() == fiber.MethodPut || c.Method() == fiber.MethodPost || c.Method() == fiber.MethodPatch ||
		c.Method() == fiber.MethodDelete || c.Method() == "MKCOL" || c.Method() == "MOVE" || c.Method() == "COPY"
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
	if write || s.auth.runtime != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "missing or invalid token")
	}
	return c.Next()
}

func (s devServer) GetFile(c *fiber.Ctx, params devapi.GetFileParams) error {
	return s.sendFile(c, params.Path)
}

func (s devServer) HeadFile(c *fiber.Ctx, params devapi.HeadFileParams) error {
	return s.sendFile(c, params.Path)
}

func (s devServer) OptionsFile(c *fiber.Ctx, _ devapi.OptionsFileParams) error {
	c.Set("DAV", "1")
	c.Set("Allow", "OPTIONS, GET, HEAD, PUT")
	return c.SendStatus(fiber.StatusNoContent)
}

func (s devServer) PutFile(c *fiber.Ctx, params devapi.PutFileParams) error {
	return s.writeFile(c, params.Path)
}

func (s devServer) WatchNotifications(c *fiber.Ctx) error {
	return websocket.New(func(conn *websocket.Conn) {
		defer conn.Close()
		sub := s.broadcast.Subscribe()
		if sub == nil {
			return
		}
		defer s.broadcast.Unsubscribe(sub)
		ping := time.NewTicker(30 * time.Second)
		defer ping.Stop()
		for {
			select {
			case msg, ok := <-sub:
				if !ok || msg == nil {
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, *msg); err != nil {
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

func (s devServer) sendFile(c *fiber.Ctx, raw string) error {
	fullPath, err := devFilePath(s.root, raw)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fiber.NewError(fiber.StatusNotFound, err.Error())
		}
		return err
	}
	if contentType := mime.TypeByExtension(filepath.Ext(fullPath)); contentType != "" {
		c.Set(fiber.HeaderContentType, contentType)
	}
	c.Set(fiber.HeaderContentLength, strconv.Itoa(len(data)))
	if c.Method() == fiber.MethodHead {
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Send(data)
}

func (s devServer) writeFile(c *fiber.Ctx, raw string) error {
	fullPath, err := devFilePath(s.root, raw)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(fullPath, c.Body(), 0644); err != nil {
		return err
	}
	s.queue.Trigger()
	return c.SendStatus(fiber.StatusNoContent)
}

func devFilePath(root string, raw string) (string, error) {
	cleaned := filepath.Clean(strings.TrimPrefix(raw, "/"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid path %q", raw)
	}
	full := filepath.Join(root, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("invalid path %q", raw)
	}
	return full, nil
}

type devTriggerQueue struct {
	broadcast *domain.BroadcastChannel
	commands  []string
}

func (q *devTriggerQueue) AddTempItem(string) error { return q.Trigger() }
func (q *devTriggerQueue) AddTempItemWithWait(command string) error {
	return q.runCommand(command)
}
func (q *devTriggerQueue) GetQueue() map[string]domain.ScrollLockStatus {
	return nil
}

func (q *devTriggerQueue) Trigger() error {
	for _, command := range q.commands {
		q.broadcastEvent("build-started")
		err := q.runCommand(command)
		q.broadcastEvent("build-ended")
		if err != nil {
			return err
		}
	}
	return nil
}

func (q *devTriggerQueue) broadcastEvent(name string) {
	data, _ := json.Marshal(map[string]any{"command_key": name, "timestamp": time.Now()})
	q.broadcast.Broadcast(data)
}

func (q *devTriggerQueue) runCommand(command string) error {
	if command == "" {
		return nil
	}
	if devDaemonURL == "" {
		return fmt.Errorf("dev daemon URL is required to run %s", command)
	}
	client, err := api.NewClientWithResponses(devDaemonURL, api.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
		if devDaemonToken != "" {
			req.Header.Set("Authorization", "Bearer "+devDaemonToken)
		}
		return nil
	}))
	if err != nil {
		return err
	}
	res, err := client.RunScrollCommandWithResponse(context.Background(), devRuntimeID, command)
	if err != nil {
		return err
	}
	if res.StatusCode() < 200 || res.StatusCode() >= 300 {
		return fmt.Errorf("run command %s failed: %s", command, res.Status())
	}
	return nil
}

type devScrollService struct {
	commands []string
}

func (s devScrollService) GetCommand(cmd string) (*domain.CommandInstructionSet, error) {
	for _, command := range s.commands {
		if command == cmd {
			return &domain.CommandInstructionSet{}, nil
		}
	}
	return nil, fmt.Errorf("command %s not found", cmd)
}
func (s devScrollService) GetCurrent() *domain.Scroll { return nil }
func (s devScrollService) GetFile() *domain.File      { return &domain.File{} }
func (s devScrollService) GetDir() string             { return "" }
func (s devScrollService) GetCwd() string             { return "" }
