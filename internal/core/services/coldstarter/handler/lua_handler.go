package lua

import (
	"fmt"
	"time"

	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type LuaHandler struct {
	file            string
	luaPath         string
	externalVars    map[string]string
	ports           map[string]int
	finishedAt      *time.Time
	queueManager    ports.QueueManagerInterface
	snapshotService ports.SnapshotService
	stateWrapper    *LuaWrapper
}

type LuaWrapper struct {
	luaState *lua.LState
}

func NewLuaHandler(queueManager ports.QueueManagerInterface, snapshotService ports.SnapshotService,
	file string, luaPath string, externalVars map[string]string, ports map[string]int) *LuaHandler {

	handler := &LuaHandler{
		file:            file,
		luaPath:         luaPath,
		externalVars:    externalVars,
		ports:           ports,
		queueManager:    queueManager,
		snapshotService: snapshotService,
		stateWrapper:    nil,
	}
	return handler
}

func (handler *LuaHandler) SetFinishedAt(finishedAt *time.Time) {
	handler.finishedAt = finishedAt
}

func (handler *LuaHandler) GetHandler(funcs map[string]func(data ...string)) (ports.ColdStarterServerInterface, error) {

	var l *lua.LState

	if handler.stateWrapper != nil {
		l = handler.stateWrapper.luaState
	} else {
		l = lua.NewState(lua.Options{
			RegistrySize: 256 * 40,
		})
	}

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
			logger.Log().Debug(arg)
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

	l.SetGlobal("get_finish_sec", l.NewFunction(
		func(l *lua.LState) int {
			if handler.finishedAt == nil {
				l.Push(lua.LNil)
			} else {
				finishSinceSec := time.Since(*handler.finishedAt).Seconds()
				l.Push(lua.LNumber(finishSinceSec))
			}
			return 1
		},
	))

	l.SetGlobal("get_port", l.NewFunction(
		func(l *lua.LState) int {
			arg := l.CheckString(1)
			ports := handler.ports

			p, ok := ports[arg]
			if !ok {
				l.Push(lua.LNil)
			} else {
				l.Push(lua.LNumber(p))
			}

			return 1
		},
	))

	l.SetGlobal("get_queue", l.NewFunction(
		func(l *lua.LState) int {
			if handler.queueManager == nil {
				return 0
			}

			queueMap := handler.queueManager.GetQueue()
			table := l.NewTable()

			for key, value := range queueMap {
				l.SetField(table, key, lua.LString(value))
			}

			l.Push(table)
			return 1
		},
	))

	l.SetGlobal("get_snapshot_percentage", l.NewFunction(
		func(l *lua.LState) int {
			progressTracker := handler.snapshotService.GetProgressTracker()
			if progressTracker == nil {
				l.Push(lua.LNumber(100))
				return 1
			}
			percent := (*progressTracker).GetPercent()
			l.Push(lua.LNumber(percent))
			return 1
		},
	))

	l.SetGlobal("get_snapshot_mode", l.NewFunction(
		func(l *lua.LState) int {
			mode := handler.snapshotService.GetCurrentMode()
			l.Push(lua.LString(mode))
			return 1
		},
	))

	if handler.stateWrapper == nil {
		// set package.path to include the luaPath
		l.DoString(fmt.Sprintf("package.path = package.path .. ';;%s/?.lua'", handler.luaPath))

		if err := l.DoFile(handler.file); err != nil {
			return nil, err
		}
	}

	handler.stateWrapper = &LuaWrapper{luaState: l}

	return handler.stateWrapper, nil
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
