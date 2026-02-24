package lua

import (
	"fmt"
	"sync"
	"time"

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
	ports        map[string]int
	finishedAt   *time.Time
	queueManager ports.QueueManagerInterface
	stateWrapper *LuaWrapper
	execWg       *sync.WaitGroup
	closed       bool
	progress     *domain.SnapshotProgress
}

type LuaWrapper struct {
	luaState *lua.LState
	execWg   *sync.WaitGroup
	closed   *bool
}

func NewLuaHandler(queueManager ports.QueueManagerInterface,
	file string, luaPath string, externalVars map[string]string, ports map[string]int, progress *domain.SnapshotProgress) *LuaHandler {

	handler := &LuaHandler{
		file:         file,
		luaPath:      luaPath,
		externalVars: externalVars,
		ports:        ports,
		queueManager: queueManager,
		stateWrapper: nil,
		execWg:       &sync.WaitGroup{},
		closed:       false,
		progress:     progress,
	}
	return handler
}

func (handler *LuaHandler) SetFinishedAt(finishedAt *time.Time) {
	handler.finishedAt = finishedAt
}

func (handler *LuaHandler) Close() error {
	//gopher-lua goes not officially support state in multiple goroutines
	//so we need to do some tricks to ensure close does not panic
	handler.closed = true
	if handler.stateWrapper != nil {
		handler.execWg.Wait()
		handler.stateWrapper.luaState.Close()
		return nil
	}
	return nil
}

func (handler *LuaHandler) GetHandler(funcs map[string]func(data ...string)) (ports.ColdStarterPacketHandlerInterface, error) {

	var l *lua.LState

	if handler.stateWrapper != nil {
		l = handler.stateWrapper.luaState
	} else {
		l = lua.NewState(lua.Options{
			RegistrySize: 256 * 40,
		})
	}
	if l.IsClosed() {
		return nil, fmt.Errorf("lua state is closed")
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
			if handler.progress != nil {
				l.Push(lua.LNumber(handler.progress.Percentage.Load()))
			} else {
				l.Push(lua.LNumber(100))
			}
			return 1
		},
	))

	l.SetGlobal("get_snapshot_mode", l.NewFunction(
		func(l *lua.LState) int {
			if handler.progress != nil {
				l.Push(lua.LString(handler.progress.Mode.Load().(string)))
			} else {
				l.Push(lua.LString("noop"))
			}
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

	handler.stateWrapper = &LuaWrapper{luaState: l, execWg: handler.execWg, closed: &handler.closed}

	return handler.stateWrapper, nil
}

func (handler *LuaWrapper) Handle(data []byte, funcs map[string]func(data ...string)) error {
	if handler.luaState.IsClosed() {
		return fmt.Errorf("lua state is closed")
	}
	//call handler function
	if err := handler.callLuaFunction(handler.luaState, "handle", funcs, data); err != nil {
		return err
	}

	return nil
}

func (handler *LuaWrapper) callLuaFunction(l *lua.LState, functionName string, sendFunc map[string]func(data ...string), args ...interface{}) error {

	if *handler.closed {
		return fmt.Errorf("lua state is closed")
	}

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
		switch a := arg.(type) {
		case []byte:
			luaArgs = append(luaArgs, lua.LString(string(a)))
		case string:
			luaArgs = append(luaArgs, lua.LString(a))
		case int:
			luaArgs = append(luaArgs, lua.LNumber(a))
		default:
			return fmt.Errorf("unsupported argument type: %T", arg)
		}
	}

	handler.execWg.Add(1)
	defer handler.execWg.Done()
	if err := l.CallByParam(lua.P{
		Fn:      l.GetGlobal(functionName),
		NRet:    len(luaArgs),
		Protect: true,
	}, luaArgs...); err != nil {
		return err
	}
	return nil
}
