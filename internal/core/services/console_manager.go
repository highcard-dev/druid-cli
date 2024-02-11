package services

import (
	"io"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/logger"
)

type ConsoleManager struct {
	consoles   map[string]*domain.Console
	logManager *LogManager
}

func NewConsoleManager(logManager *LogManager) *ConsoleManager {
	return &ConsoleManager{
		consoles:   make(map[string]*domain.Console),
		logManager: logManager,
	}
}

func (cm *ConsoleManager) AddConsole(id string, consoleType string, inputMode string, consoleReader io.Reader) *domain.Console {
	var newChannel *domain.BroadcastChannel
	var console *domain.Console

	if _, ok := cm.consoles[id]; !ok {
		newChannel = domain.NewHub()

		console := &domain.Console{
			Channel:   newChannel,
			Type:      consoleType,
			InputMode: inputMode,
		}

		cm.consoles[id] = console

		go newChannel.Run()
	} else {
		console = cm.consoles[id]
		newChannel = cm.consoles[id].Channel
	}

	//broadcast reader into channel (maybe increase chunk size?)
	go func() {
		for {

			//io.Reader to channel chunks
			tmpBuffer := make([]byte, 4096)
			n, err := consoleReader.Read(tmpBuffer)

			if err != nil {
				cm.RemoveConsole(id)
				return
			}

			if consoleType != "tty" {
				logger.Log().Info(string(tmpBuffer[:n]))
			}

			cm.logManager.AddLine(id, tmpBuffer[:n])

			newChannel.Broadcast <- tmpBuffer[:n]
		}
	}()
	return console
}

func (cm *ConsoleManager) AddConsoleWithChannel(id string, consoleType string, inputMode string, channel chan string) *domain.Console {

	newChannel := domain.NewHub()

	console := &domain.Console{
		Channel:   newChannel,
		Type:      consoleType,
		InputMode: inputMode,
	}

	cm.consoles[id] = console

	go newChannel.Run()

	//broadcast reader into channel (maybe increase chunk size?)
	go func() {
		for {
			select {
			case data := <-channel:
				b := []byte(data)
				newChannel.Broadcast <- b
			}
		}
	}()

	return console
}

func (cm *ConsoleManager) RemoveConsole(id string) {

	cm.consoles[id].Channel.CloseChannel()

	delete(cm.consoles, id)
}

func (cm *ConsoleManager) GetSubscription(id string) chan *[]byte {
	if _, ok := cm.consoles[id]; !ok {
		return nil
	}

	c := cm.consoles[id].Channel.Subscribe()
	return c
}

func (cm *ConsoleManager) DeleteSubscription(id string, subscription chan *[]byte) {
	cm.consoles[id].Channel.Unsubscribe(subscription)
}

func (cm *ConsoleManager) GetConsoles() map[string]*domain.Console {
	return cm.consoles
}
