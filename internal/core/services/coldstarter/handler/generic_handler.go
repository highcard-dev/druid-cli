package lua

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type GenericHandler struct{}

func NewGenericHandler() *GenericHandler {
	return &GenericHandler{}
}

func (handler *GenericHandler) Handle(data []byte, funcs map[string]func(data ...string)) error {
	finishFunc, ok := funcs["finish"]

	if !ok {
		return fmt.Errorf("finish function not found")
	}

	logger.Log().Info("Executing finish func in generic handler", zap.String("data", string(data)))

	finishFunc()
	return nil
}
