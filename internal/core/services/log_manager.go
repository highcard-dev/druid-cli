package services

import (
	"container/list"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func NewLog(capacity uint) *domain.Log {
	writer := make(chan domain.StreamCommand, 1024)
	h := &domain.Log{
		List:     list.New(),
		Capacity: capacity,
		Req:      make(chan chan<- domain.StreamCommand),
		Write:    writer,
	}

	go func(write <-chan domain.StreamCommand) {
		for {
			select {
			case res := <-h.Req:
				for e := h.List.Front(); e != nil; e = e.Next() {
					res <- e.Value.(domain.StreamCommand)
				}
				close(res)
			case in, ok := <-write:
				push := func(in domain.StreamCommand, ok bool) bool {
					if !ok {
						return false
					}
					if h.List.Len() >= int(h.Capacity) {
						h.List.Remove(h.List.Front())
					}
					h.List.PushBack(in)
					return true
				}
				push(in, ok)

				//empty entire write queue
			drain:
				for {
					select {
					case in, ok = <-write:
						if !push(in, ok) {
							break drain
						}
					default:
						break drain
					}
				}
			}
		}
	}(writer)

	return h
}

type LogManager struct {
	Streams map[string]*domain.Log
}

func NewLogManager() *LogManager {
	return &LogManager{
		Streams: make(map[string]*domain.Log),
	}
}

func (hm *LogManager) AddLine(stream string, sc domain.StreamCommand) {
	if _, ok := hm.Streams[stream]; !ok {
		hm.Streams[stream] = NewLog(100)
	}
	hm.Streams[stream].Write <- sc
}

func (hm *LogManager) GetStreams() map[string]*domain.Log {
	return hm.Streams
}
