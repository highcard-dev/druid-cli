package lua

import "fmt"

type GenericHandler struct{}

func NewGenericHandler() *GenericHandler {
	return &GenericHandler{}
}

func (handler *GenericHandler) Handle(data []byte, funcs map[string]func(data ...string)) error {
	finishFunc, ok := funcs["finish"]

	if !ok {
		return fmt.Errorf("finish function not found")
	}

	finishFunc()
	return nil
}
