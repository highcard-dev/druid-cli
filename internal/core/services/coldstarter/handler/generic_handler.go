package lua

import (
	"fmt"
	"time"

	"github.com/highcard-dev/daemon/internal/core/ports"
)

type GenericHandler struct {
	finishFunc func(data ...string)
}

func (handler *GenericHandler) Handle(data []byte, funcs map[string]func(...string)) error {
	handler.finishFunc()
	return nil
}

type GenericReturnHandler struct{}

func NewGenericReturnHandler() *GenericReturnHandler {
	return &GenericReturnHandler{}
}

func (handler *GenericReturnHandler) GetHandler(funcs map[string]func(data ...string)) (ports.ColdStarterServerInterface, error) {
	finishFunc, ok := funcs["finish"]
	if !ok {
		return nil, fmt.Errorf("finish function not found")
	}
	return &GenericHandler{
		finishFunc: finishFunc,
	}, nil
}

func (handler *GenericReturnHandler) SetFinishedAt(finishedAt *time.Time) {}
