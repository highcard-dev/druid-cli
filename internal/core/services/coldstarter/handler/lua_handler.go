package lua

import (
	"fmt"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type LuaHandler struct {
	file         string
	luaPath      string
	externalVars map[string]string
	ports        []domain.Port
}

type LuaWrapper struct {
	luaState *lua.LState
}

func NewLuaHandler(file string, luaPath string, externalVars map[string]string, ports []domain.Port) *LuaHandler {

	handler := &LuaHandler{
		file:         file,
		luaPath:      luaPath,
		externalVars: externalVars,
		ports:        ports,
	}
	return handler
}

func (handler *LuaHandler) GetHandler(funcs map[string]func(data ...string)) (ports.ColdStarterHandlerInterface, error) {

	l := lua.NewState(lua.Options{
		RegistrySize: 256 * 200,
	})

	for name, f := range funcs {
		// Create a new variable to capture the current function
		currentFunc := f

		var fn *lua.LFunction

		switch name {
		case "sendData":
			fn = l.NewFunction(func(l *lua.LState) int {
				arg := l.CheckString(1)
				logger.Log().Debug("Called lua fn sendData", zap.String("arg", arg), zap.String("file", handler.file))
				currentFunc(arg)
				return 1
			})
		case "finish":
			fn = l.NewFunction(func(l *lua.LState) int {
				logger.Log().Debug("Called lua fn sendData", zap.String("file", handler.file))
				currentFunc()
				return 0
			})
		case "close":
			fn = l.NewFunction(func(l *lua.LState) int {
				arg := l.CheckString(1)
				logger.Log().Debug("Called lua fn sendData", zap.String("arg", arg), zap.String("file", handler.file))
				currentFunc(arg)
				return 1
			})
		default:
			return nil, fmt.Errorf("unsupported function: %s", name)
		}
		l.SetGlobal(name, fn)
	}

	l.SetGlobal("debug_print", l.NewFunction(
		func(l *lua.LState) int {
			arg := l.CheckString(1)
			logger.Log().Info(arg)
			return 0
		},
	))

	l.SetGlobal("get_var", l.NewFunction(
		func(l *lua.LState) int {
			arg := l.CheckString(1)

			//get external var
			value, ok := handler.externalVars[arg]
			if !ok {
				l.Push(lua.LNil)
			} else {
				l.Push(lua.LString(value))
			}
			return 1
		},
	))

	l.SetGlobal("get_port", l.NewFunction(
		func(l *lua.LState) int {
			arg := l.CheckString(1)
			ports := handler.ports

			for _, port := range ports {
				if port.Name == arg {
					l.Push(lua.LNumber(port.Port))
					return 1
				}
			}

			l.Push(lua.LNil)
			return 1
		},
	))

	// set package.path to include the luaPath
	l.DoString(fmt.Sprintf("package.path = package.path .. ';;%s/?.lua'", handler.luaPath))

	if err := l.DoFile(handler.file); err != nil {
		return nil, err
	}

	return &LuaWrapper{luaState: l}, nil
}

func (handler *LuaWrapper) Handle(data []byte, funcs map[string]func(data ...string)) error {
	//call handler function
	if err := callLuaFunction(handler.luaState, "handle", funcs, data); err != nil {
		return err
	}

	return nil
}

func callLuaFunction(l *lua.LState, functionName string, sendFunc map[string]func(data ...string), args ...interface{}) error {
	var luaArgs []lua.LValue

	//first argument is a table of functions

	table := l.NewTable()

	for name, f := range sendFunc {
		// Create a new variable to capture the current function
		currentFunc := f

		var fn *lua.LFunction

		switch name {
		case "sendData":
			fn = l.NewFunction(func(l *lua.LState) int {
				arg := l.CheckString(1)
				logger.Log().Debug("Called lua fn sendData", zap.String("arg", arg))
				currentFunc(arg)
				return 1
			})
		}

		table.RawSetString(name, fn)
	}

	luaArgs = append(luaArgs, table)

	for _, arg := range args {
		switch arg.(type) {
		case []byte:
			luaArgs = append(luaArgs, lua.LString(string(arg.([]byte))))
		case string:
			luaArgs = append(luaArgs, lua.LString(arg.(string)))
		case int:
			luaArgs = append(luaArgs, lua.LNumber(arg.(int)))
		default:
			return fmt.Errorf("unsupported argument type: %T", arg)
		}
	}

	if err := l.CallByParam(lua.P{
		Fn:      l.GetGlobal(functionName),
		NRet:    len(luaArgs),
		Protect: true,
	}, luaArgs...); err != nil {
		return err
	}
	return nil
}
