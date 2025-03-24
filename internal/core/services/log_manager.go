package services

import (
	"container/list"

	"github.com/highcard-dev/daemon/internal/core/domain"
)

func NewLog(capacity uint) *domain.Log {
	writer := make(chan []byte, 1024)
	h := &domain.Log{
		List:     list.New(),
		Capacity: capacity,
		Req:      make(chan chan<- []byte),
		Write:    writer,
	}

	go func(write <-chan []byte) {
		for {
			select {
			case res := <-h.Req:
				for e := h.List.Front(); e != nil; e = e.Next() {
					res <- e.Value.([]byte)
				}
				close(res)
			case in, ok := <-write:
				push := func(in []byte, ok bool) bool {
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

func (hm *LogManager) AddLine(stream string, line []byte) {

	println("log: " + string(line))
	if _, ok := hm.Streams[stream]; !ok {
		hm.Streams[stream] = NewLog(100)
	}
	hm.Streams[stream].Write <- line
}

func (hm *LogManager) GetStreams() map[string]*domain.Log {
	return hm.Streams
}
