package servers

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type UDPServer interface {
	Start(port int)
}

type UDP struct {
	handler  ports.ColdStarterHandlerInterface
	conn     *net.UDPConn
	onFinish func()
}

func NewUDP(handler ports.ColdStarterHandlerInterface) *UDP {
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
					logger.Log().Info("UDP Server stopped", zap.Error(err))
					return
				}
				logger.Log().Warn("Error reading from UDP connection", zap.Error(err))
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

	handler, err := u.handler.GetHandler(map[string]func(data ...string){
		"finish": func(data ...string) {
			fmt.Println("Connection closed")
			logger.Log().Info("Finish received", zap.Strings("data", data), zap.String("type", "udp"), zap.String("address", remoteAddr.String()))
			<-time.After(time.Second)
			u.onFinish()
			<-time.After(time.Second)
			u.conn.Close()
		},
	})

	if err != nil {
		logger.Log().Error("Error getting handler", zap.Error(err))
		return
	}

	err = handler.Handle(data, map[string]func(data ...string){
		"sendData": sendFunc,
	})

	if err != nil {
		logger.Log().Error("Error handling packet", zap.Error(err))
	}
}
