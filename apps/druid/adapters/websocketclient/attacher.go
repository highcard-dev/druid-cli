package websocketclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	gw "github.com/gorilla/websocket"
	"github.com/highcard-dev/daemon/internal/utils"
)

type Attacher struct {
	daemonSocket string
}

func NewAttacher(daemonSocket string) *Attacher {
	return &Attacher{daemonSocket: daemonSocket}
}

func (a *Attacher) Attach(ctx context.Context, scroll string, console string) error {
	wsURL, err := a.websocketURL(scroll, console)
	if err != nil {
		return err
	}
	daemonSocket := a.daemonSocket
	if daemonSocket == "" {
		daemonSocket = utils.DefaultRuntimeSocketPath()
	}
	dialer := &gw.Dialer{
		NetDialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", daemonSocket)
		},
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	done := make(chan error, 2)
	go readOutput(conn, done)
	go writeInput(conn, done)

	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		return err
	}
}

func (a *Attacher) websocketURL(scroll string, console string) (string, error) {
	return fmt.Sprintf("ws://druid/ws/v1/scrolls/%s/consoles/%s", url.PathEscape(scroll), url.PathEscape(console)), nil
}

func readOutput(conn *gw.Conn, done chan<- error) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			done <- err
			return
		}
		if _, err := os.Stdout.Write(data); err != nil {
			done <- err
			return
		}
	}
}

func writeInput(conn *gw.Conn, done chan<- error) {
	buf := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if writeErr := conn.WriteMessage(gw.TextMessage, buf[:n]); writeErr != nil {
				done <- writeErr
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				done <- nil
			} else {
				done <- err
			}
			return
		}
	}
}
