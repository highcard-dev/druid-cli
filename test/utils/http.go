package test_utils

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func CheckHttpServerShutdown(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", "localhost:"+strconv.Itoa(port))
		if err != nil {
			// Connection successful, close and return no error
			return nil
		}
		c.Close()
		// Wait for 1 second before retrying
		time.Sleep(1 * time.Second)
	}
	return errors.New("timeout reached while checking HTTP server")
}

func FetchBytes(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.Bytes(), nil
}

func ConnectWebsocket(addr string, console string) (*websocket.Conn, error) {

	u := url.URL{Scheme: "ws", Host: addr, Path: console}

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	return c, err
}
func WaitForWebsocketConnection(host string, path string, duration time.Duration) (*websocket.Conn, error) {

	//connect to ws server and check logs
	var wsClient *websocket.Conn
	var err error

	timeout := time.After(duration)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return nil, errors.New("timeout waiting for ws connection")
		case <-ticker.C:
			wsClient, err = ConnectWebsocket(host, path)
			if err == nil {
				return wsClient, nil
			}
		}
	}
}

func WaitForWebsocketMessage(wsClient *websocket.Conn, message string, timeout time.Duration) error {
	for {
		select {
		case <-time.After(timeout):
			return fmt.Errorf("timeout waiting for message: %s", message)
		default:
			_, readMsg, err := wsClient.ReadMessage()
			if err != nil {
				return err
			}
			//print(string(readMsg))
			if strings.Contains(string(readMsg), message) {
				return nil
			}
		}
	}
}

func CheckHttpServer(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", "localhost:"+strconv.Itoa(port))
		if err == nil {
			// Connection successful, close and return no error
			c.Close()
			return nil
		}
		// Wait for 1 second before retrying
		time.Sleep(1 * time.Second)
	}
	return errors.New("timeout reached while checking HTTP server")
}
