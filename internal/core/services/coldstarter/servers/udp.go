package servers

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/highcard-dev/daemon/internal/core/services/coldstarter/lua"
)

type UDPServer interface {
	Start(port int)
}

type UDP struct {
	handler  *lua.LuaHandler
	conn     *net.UDPConn
	onFinish func()
}

func NewUDP(handler *lua.LuaHandler) *UDP {
	return &UDP{
		handler: handler,
	}
}

func (u *UDP) Start(ctx context.Context, port int, onFinish func()) error {
	addr := net.UDPAddr{
		Port: port,
		IP:   net.IPv4zero,
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return fmt.Errorf("failed to bind [%v]", err)
	}
	u.conn = conn
	u.onFinish = onFinish

	go func() {
		buf := make([]byte, 1024)
		for {
			n, remoteAddr, err := u.conn.ReadFromUDP(buf)
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
					log.Println("Server stopped")
					return
				}
				log.Println("Error reading from connection:", err)
				continue
			}

			go u.handleConnection(buf[:n], remoteAddr)
		}
	}()

	<-ctx.Done()
	u.conn.Close()

	return nil
}

func (u *UDP) handleConnection(data []byte, remoteAddr *net.UDPAddr) {
	sendFunc := func(data ...string) {
		if len(data) == 0 {
			return
		}
		u.conn.WriteToUDP([]byte(data[0]), remoteAddr)
	}

	err := u.handler.Handle(data, map[string]func(data ...string){
		"sendData": sendFunc,
		"finish": func(data ...string) {
			fmt.Println("Connection closed")
			u.onFinish()
		},
		"close": func(data ...string) {
			sendFunc(data...)
			//wait for 1 second before closing the connection
			<-time.After(time.Second)
			u.conn.Close()
		},
	})

	if err != nil {
		log.Println("Error handling packet:", err)
	}
}
