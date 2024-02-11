package services

import (
	"io"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

type ConsoleManager struct {
	consoles map[string]*domain.Console
}

func NewConsoleManager() *ConsoleManager {
	return &ConsoleManager{
		consoles: make(map[string]*domain.Console),
	}
}

func (cm *ConsoleManager) AddConsole(id string, consoleType string, inputMode string, consoleReader io.Reader) *domain.Console {

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

			//io.Reader to channel chunks
			tmpBuffer := make([]byte, 4096)
			n, err := consoleReader.Read(tmpBuffer)

			if err != nil {
				cm.RemoveConsole(id)
				return
			}
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

	cm.consoles[id].Channel.Close <- struct{}{}

	delete(cm.consoles, id)
}

func (cm *ConsoleManager) GetSubscription(id string) chan *[]byte {
	if _, ok := cm.consoles[id]; !ok {
		return nil
	}

	c := make(chan *[]byte)

	cm.consoles[id].Channel.Register <- c
	return c
}

func (cm *ConsoleManager) DeleteSubscription(id string, subscription chan *[]byte) {
	cm.consoles[id].Channel.Unregister <- subscription
}

func (cm *ConsoleManager) GetConsoles() map[string]*domain.Console {
	return cm.consoles
}
