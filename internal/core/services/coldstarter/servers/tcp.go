package servers

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type TCPServer interface {
	Start(port int, handlerFile string)
}

type TCP struct {
	handler  ports.ColdStarterHandlerInterface
	listener net.Listener
	onFinish func()
}

func NewTCP(handler ports.ColdStarterHandlerInterface) *TCP {
	return &TCP{
		handler: handler,
	}
}

func (t *TCP) Start(port int, onFinish func()) error {
	ser, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to resolve address [%v]", err)
	}
	tcp, err := net.ListenTCP("tcp", ser)
	if err != nil {
		return fmt.Errorf("failed to bind [%v]", err)
	}
	t.listener = tcp
	t.onFinish = onFinish

	go func() {
		for {
			con, err := tcp.AcceptTCP()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
					logger.Log().Info("TCP Server stopped")
					return
				}
				logger.Log().Warn("Error accepting TCP connection", zap.Error(err))
				continue
			}

			_ = con.SetNoDelay(true)
			_ = con.SetKeepAlive(true)
			go t.handleConnection(con)
		}
	}()

	return nil
}
func (t *TCP) handleConnection(conn net.Conn) {

	sendFunc := func(data ...string) {
		if len(data) == 0 {
			return
		}
		_, err := conn.Write([]byte(data[0]))
		if err != nil {
			logger.Log().Error("Error sending data", zap.Error(err))
		}
	}
	handler, err := t.handler.GetHandler(map[string]func(data ...string){
		"sendData": sendFunc,
		"finish": func(data ...string) {
			fmt.Println("Connection closed")
			logger.Log().Info("Finish received", zap.Strings("data", data), zap.String("type", "tcp"), zap.String("address", conn.RemoteAddr().String()))
			<-time.After(time.Second)
			t.onFinish()
			<-time.After(time.Second)
			conn.Close()
		},
		"close": func(data ...string) {
			sendFunc(data...)
			//wait for 1 second before closing the connection
			<-time.After(time.Second)
			conn.Close()
		},
	})
	if err != nil {
		logger.Log().Error("Error getting handler", zap.Error(err))
		return
	}

	reader := bufio.NewReader(conn)
	for {
		// Adjust this buffer size based on your expected packet size
		buffer := make([]byte, 1024)
		n, err := reader.Read(buffer)
		if err != nil {
			if err != io.EOF {
				logger.Log().Error("Error reading from connection", zap.Error(err))
			}
			conn.Close()
			break
		}

		data := buffer[:n]

		logger.Log().Debug("Received data", zap.String("data", string(data)), zap.String("type", "tcp"), zap.String("address", conn.RemoteAddr().String()))

		err = handler.Handle(data, map[string]func(data ...string){
			"sendData": sendFunc,
		})

		if err != nil {
			logger.Log().Error("Error handling packet", zap.Error(err))
		}
	}
}

func (t *TCP) Close() error {
	if t.listener != nil {
		err := t.listener.Close()
		if err != nil {
			return fmt.Errorf("failed to close listener [%v]", err)
		}
	}

	err := t.handler.Close()
	if err != nil {
		return fmt.Errorf("failed to close handler: %v", err)
	}

	return nil
}
