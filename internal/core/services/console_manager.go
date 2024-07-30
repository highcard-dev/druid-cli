package services

import (
	"sync"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

type ConsoleManager struct {
	consoles   map[string]*domain.Console
	logManager ports.LogManagerInterface
	mu         sync.Mutex
}

func NewConsoleManager(logManager ports.LogManagerInterface) *ConsoleManager {
	return &ConsoleManager{
		consoles:   make(map[string]*domain.Console),
		logManager: logManager,
	}
}

func (cm *ConsoleManager) AddConsoleWithChannel(id string, consoleType domain.ConsoleType, inputMode string, channel chan string) (*domain.Console, chan struct{}) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	newChannel := domain.NewHub()

	console := &domain.Console{
		Channel:   newChannel,
		Type:      consoleType,
		InputMode: inputMode,
	}

	cm.consoles[id] = console

	go newChannel.Run()

	done := make(chan struct{})

	//broadcast reader into channel (maybe increase chunk size?)
	go func() {
		for data := range channel {
			b := []byte(data)
			newChannel.Broadcast <- b
			cm.logManager.AddLine(id, b)
		}
		close(done)
	}()

	return console, done
}

func (cm *ConsoleManager) GetConsoles() map[string]*domain.Console {
	return cm.consoles
}

func (cm *ConsoleManager) GetConsole(id string) *domain.Console {
	return cm.consoles[id]
}
