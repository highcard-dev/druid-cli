// Code generated by MockGen. DO NOT EDIT.
// Source: internal/core/ports/services_ports.go
//
// Generated by this command:
//
//	mockgen -source=internal/core/ports/services_ports.go -destination test/mock/services.go
//

// Package mock_ports is a generated GoMock package.
package mock_ports

import (
	io "io"
	reflect "reflect"
	time "time"

	fiber "github.com/gofiber/fiber/v2"
	domain "github.com/highcard-dev/daemon/internal/core/domain"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	gomock "go.uber.org/mock/gomock"
	file "oras.land/oras-go/v2/content/file"
	remote "oras.land/oras-go/v2/registry/remote"
)

// MockAuthorizerServiceInterface is a mock of AuthorizerServiceInterface interface.
type MockAuthorizerServiceInterface struct {
	ctrl     *gomock.Controller
	recorder *MockAuthorizerServiceInterfaceMockRecorder
}

// MockAuthorizerServiceInterfaceMockRecorder is the mock recorder for MockAuthorizerServiceInterface.
type MockAuthorizerServiceInterfaceMockRecorder struct {
	mock *MockAuthorizerServiceInterface
}

// NewMockAuthorizerServiceInterface creates a new mock instance.
func NewMockAuthorizerServiceInterface(ctrl *gomock.Controller) *MockAuthorizerServiceInterface {
	mock := &MockAuthorizerServiceInterface{ctrl: ctrl}
	mock.recorder = &MockAuthorizerServiceInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAuthorizerServiceInterface) EXPECT() *MockAuthorizerServiceInterfaceMockRecorder {
	return m.recorder
}

// CheckHeader mocks base method.
func (m *MockAuthorizerServiceInterface) CheckHeader(r *fiber.Ctx) (*time.Time, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CheckHeader", r)
	ret0, _ := ret[0].(*time.Time)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CheckHeader indicates an expected call of CheckHeader.
func (mr *MockAuthorizerServiceInterfaceMockRecorder) CheckHeader(r any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CheckHeader", reflect.TypeOf((*MockAuthorizerServiceInterface)(nil).CheckHeader), r)
}

// CheckQuery mocks base method.
func (m *MockAuthorizerServiceInterface) CheckQuery(token string) (*time.Time, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CheckQuery", token)
	ret0, _ := ret[0].(*time.Time)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CheckQuery indicates an expected call of CheckQuery.
func (mr *MockAuthorizerServiceInterfaceMockRecorder) CheckQuery(token any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CheckQuery", reflect.TypeOf((*MockAuthorizerServiceInterface)(nil).CheckQuery), token)
}

// GenerateQueryToken mocks base method.
func (m *MockAuthorizerServiceInterface) GenerateQueryToken() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GenerateQueryToken")
	ret0, _ := ret[0].(string)
	return ret0
}

// GenerateQueryToken indicates an expected call of GenerateQueryToken.
func (mr *MockAuthorizerServiceInterfaceMockRecorder) GenerateQueryToken() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GenerateQueryToken", reflect.TypeOf((*MockAuthorizerServiceInterface)(nil).GenerateQueryToken))
}

// MockScrollServiceInterface is a mock of ScrollServiceInterface interface.
type MockScrollServiceInterface struct {
	ctrl     *gomock.Controller
	recorder *MockScrollServiceInterfaceMockRecorder
}

// MockScrollServiceInterfaceMockRecorder is the mock recorder for MockScrollServiceInterface.
type MockScrollServiceInterfaceMockRecorder struct {
	mock *MockScrollServiceInterface
}

// NewMockScrollServiceInterface creates a new mock instance.
func NewMockScrollServiceInterface(ctrl *gomock.Controller) *MockScrollServiceInterface {
	mock := &MockScrollServiceInterface{ctrl: ctrl}
	mock.recorder = &MockScrollServiceInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockScrollServiceInterface) EXPECT() *MockScrollServiceInterfaceMockRecorder {
	return m.recorder
}

// GetCommand mocks base method.
func (m *MockScrollServiceInterface) GetCommand(cmd, processId string) (*domain.CommandInstructionSet, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCommand", cmd, processId)
	ret0, _ := ret[0].(*domain.CommandInstructionSet)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetCommand indicates an expected call of GetCommand.
func (mr *MockScrollServiceInterfaceMockRecorder) GetCommand(cmd, processId any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCommand", reflect.TypeOf((*MockScrollServiceInterface)(nil).GetCommand), cmd, processId)
}

// GetCurrent mocks base method.
func (m *MockScrollServiceInterface) GetCurrent() *domain.Scroll {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCurrent")
	ret0, _ := ret[0].(*domain.Scroll)
	return ret0
}

// GetCurrent indicates an expected call of GetCurrent.
func (mr *MockScrollServiceInterfaceMockRecorder) GetCurrent() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCurrent", reflect.TypeOf((*MockScrollServiceInterface)(nil).GetCurrent))
}

// GetCwd mocks base method.
func (m *MockScrollServiceInterface) GetCwd() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetCwd")
	ret0, _ := ret[0].(string)
	return ret0
}

// GetCwd indicates an expected call of GetCwd.
func (mr *MockScrollServiceInterfaceMockRecorder) GetCwd() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetCwd", reflect.TypeOf((*MockScrollServiceInterface)(nil).GetCwd))
}

// GetDir mocks base method.
func (m *MockScrollServiceInterface) GetDir() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetDir")
	ret0, _ := ret[0].(string)
	return ret0
}

// GetDir indicates an expected call of GetDir.
func (mr *MockScrollServiceInterfaceMockRecorder) GetDir() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetDir", reflect.TypeOf((*MockScrollServiceInterface)(nil).GetDir))
}

// GetFile mocks base method.
func (m *MockScrollServiceInterface) GetFile() *domain.File {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetFile")
	ret0, _ := ret[0].(*domain.File)
	return ret0
}

// GetFile indicates an expected call of GetFile.
func (mr *MockScrollServiceInterfaceMockRecorder) GetFile() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetFile", reflect.TypeOf((*MockScrollServiceInterface)(nil).GetFile))
}

// GetLock mocks base method.
func (m *MockScrollServiceInterface) GetLock() (*domain.ScrollLock, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLock")
	ret0, _ := ret[0].(*domain.ScrollLock)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetLock indicates an expected call of GetLock.
func (mr *MockScrollServiceInterfaceMockRecorder) GetLock() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLock", reflect.TypeOf((*MockScrollServiceInterface)(nil).GetLock))
}

// GetScrollConfigRawYaml mocks base method.
func (m *MockScrollServiceInterface) GetScrollConfigRawYaml() []byte {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetScrollConfigRawYaml")
	ret0, _ := ret[0].([]byte)
	return ret0
}

// GetScrollConfigRawYaml indicates an expected call of GetScrollConfigRawYaml.
func (mr *MockScrollServiceInterfaceMockRecorder) GetScrollConfigRawYaml() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetScrollConfigRawYaml", reflect.TypeOf((*MockScrollServiceInterface)(nil).GetScrollConfigRawYaml))
}

// WriteNewScrollLock mocks base method.
func (m *MockScrollServiceInterface) WriteNewScrollLock() *domain.ScrollLock {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WriteNewScrollLock")
	ret0, _ := ret[0].(*domain.ScrollLock)
	return ret0
}

// WriteNewScrollLock indicates an expected call of WriteNewScrollLock.
func (mr *MockScrollServiceInterfaceMockRecorder) WriteNewScrollLock() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WriteNewScrollLock", reflect.TypeOf((*MockScrollServiceInterface)(nil).WriteNewScrollLock))
}

// MockProcedureLauchnerInterface is a mock of ProcedureLauchnerInterface interface.
type MockProcedureLauchnerInterface struct {
	ctrl     *gomock.Controller
	recorder *MockProcedureLauchnerInterfaceMockRecorder
}

// MockProcedureLauchnerInterfaceMockRecorder is the mock recorder for MockProcedureLauchnerInterface.
type MockProcedureLauchnerInterfaceMockRecorder struct {
	mock *MockProcedureLauchnerInterface
}

// NewMockProcedureLauchnerInterface creates a new mock instance.
func NewMockProcedureLauchnerInterface(ctrl *gomock.Controller) *MockProcedureLauchnerInterface {
	mock := &MockProcedureLauchnerInterface{ctrl: ctrl}
	mock.recorder = &MockProcedureLauchnerInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockProcedureLauchnerInterface) EXPECT() *MockProcedureLauchnerInterfaceMockRecorder {
	return m.recorder
}

// RunNew mocks base method.
func (m *MockProcedureLauchnerInterface) RunNew(commandId, processId string, changeStatus bool) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RunNew", commandId, processId, changeStatus)
	ret0, _ := ret[0].(error)
	return ret0
}

// RunNew indicates an expected call of RunNew.
func (mr *MockProcedureLauchnerInterfaceMockRecorder) RunNew(commandId, processId, changeStatus any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RunNew", reflect.TypeOf((*MockProcedureLauchnerInterface)(nil).RunNew), commandId, processId, changeStatus)
}

// RunProcedure mocks base method.
func (m *MockProcedureLauchnerInterface) RunProcedure(arg0 *domain.Procedure, arg1, arg2 string) (string, *int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RunProcedure", arg0, arg1, arg2)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(*int)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// RunProcedure indicates an expected call of RunProcedure.
func (mr *MockProcedureLauchnerInterfaceMockRecorder) RunProcedure(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RunProcedure", reflect.TypeOf((*MockProcedureLauchnerInterface)(nil).RunProcedure), arg0, arg1, arg2)
}

// MockPluginManagerInterface is a mock of PluginManagerInterface interface.
type MockPluginManagerInterface struct {
	ctrl     *gomock.Controller
	recorder *MockPluginManagerInterfaceMockRecorder
}

// MockPluginManagerInterfaceMockRecorder is the mock recorder for MockPluginManagerInterface.
type MockPluginManagerInterfaceMockRecorder struct {
	mock *MockPluginManagerInterface
}

// NewMockPluginManagerInterface creates a new mock instance.
func NewMockPluginManagerInterface(ctrl *gomock.Controller) *MockPluginManagerInterface {
	mock := &MockPluginManagerInterface{ctrl: ctrl}
	mock.recorder = &MockPluginManagerInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPluginManagerInterface) EXPECT() *MockPluginManagerInterfaceMockRecorder {
	return m.recorder
}

// CanRunStandaloneProcedure mocks base method.
func (m *MockPluginManagerInterface) CanRunStandaloneProcedure(mode string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CanRunStandaloneProcedure", mode)
	ret0, _ := ret[0].(bool)
	return ret0
}

// CanRunStandaloneProcedure indicates an expected call of CanRunStandaloneProcedure.
func (mr *MockPluginManagerInterfaceMockRecorder) CanRunStandaloneProcedure(mode any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CanRunStandaloneProcedure", reflect.TypeOf((*MockPluginManagerInterface)(nil).CanRunStandaloneProcedure), mode)
}

// GetNotifyConsoleChannel mocks base method.
func (m *MockPluginManagerInterface) GetNotifyConsoleChannel() chan *domain.StreamItem {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNotifyConsoleChannel")
	ret0, _ := ret[0].(chan *domain.StreamItem)
	return ret0
}

// GetNotifyConsoleChannel indicates an expected call of GetNotifyConsoleChannel.
func (mr *MockPluginManagerInterfaceMockRecorder) GetNotifyConsoleChannel() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNotifyConsoleChannel", reflect.TypeOf((*MockPluginManagerInterface)(nil).GetNotifyConsoleChannel))
}

// HasMode mocks base method.
func (m *MockPluginManagerInterface) HasMode(mode string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasMode", mode)
	ret0, _ := ret[0].(bool)
	return ret0
}

// HasMode indicates an expected call of HasMode.
func (mr *MockPluginManagerInterfaceMockRecorder) HasMode(mode any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasMode", reflect.TypeOf((*MockPluginManagerInterface)(nil).HasMode), mode)
}

// ParseFromScroll mocks base method.
func (m *MockPluginManagerInterface) ParseFromScroll(pluginDefinitionMap map[string]map[string]string, config, cwd string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ParseFromScroll", pluginDefinitionMap, config, cwd)
	ret0, _ := ret[0].(error)
	return ret0
}

// ParseFromScroll indicates an expected call of ParseFromScroll.
func (mr *MockPluginManagerInterfaceMockRecorder) ParseFromScroll(pluginDefinitionMap, config, cwd any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ParseFromScroll", reflect.TypeOf((*MockPluginManagerInterface)(nil).ParseFromScroll), pluginDefinitionMap, config, cwd)
}

// RunProcedure mocks base method.
func (m *MockPluginManagerInterface) RunProcedure(mode, value string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RunProcedure", mode, value)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RunProcedure indicates an expected call of RunProcedure.
func (mr *MockPluginManagerInterfaceMockRecorder) RunProcedure(mode, value any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RunProcedure", reflect.TypeOf((*MockPluginManagerInterface)(nil).RunProcedure), mode, value)
}

// MockLogManagerInterface is a mock of LogManagerInterface interface.
type MockLogManagerInterface struct {
	ctrl     *gomock.Controller
	recorder *MockLogManagerInterfaceMockRecorder
}

// MockLogManagerInterfaceMockRecorder is the mock recorder for MockLogManagerInterface.
type MockLogManagerInterfaceMockRecorder struct {
	mock *MockLogManagerInterface
}

// NewMockLogManagerInterface creates a new mock instance.
func NewMockLogManagerInterface(ctrl *gomock.Controller) *MockLogManagerInterface {
	mock := &MockLogManagerInterface{ctrl: ctrl}
	mock.recorder = &MockLogManagerInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockLogManagerInterface) EXPECT() *MockLogManagerInterfaceMockRecorder {
	return m.recorder
}

// AddLine mocks base method.
func (m *MockLogManagerInterface) AddLine(stream string, sc []byte) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddLine", stream, sc)
}

// AddLine indicates an expected call of AddLine.
func (mr *MockLogManagerInterfaceMockRecorder) AddLine(stream, sc any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddLine", reflect.TypeOf((*MockLogManagerInterface)(nil).AddLine), stream, sc)
}

// GetStreams mocks base method.
func (m *MockLogManagerInterface) GetStreams() map[string]*domain.Log {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetStreams")
	ret0, _ := ret[0].(map[string]*domain.Log)
	return ret0
}

// GetStreams indicates an expected call of GetStreams.
func (mr *MockLogManagerInterfaceMockRecorder) GetStreams() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStreams", reflect.TypeOf((*MockLogManagerInterface)(nil).GetStreams))
}

// MockProcessManagerInterface is a mock of ProcessManagerInterface interface.
type MockProcessManagerInterface struct {
	ctrl     *gomock.Controller
	recorder *MockProcessManagerInterfaceMockRecorder
}

// MockProcessManagerInterfaceMockRecorder is the mock recorder for MockProcessManagerInterface.
type MockProcessManagerInterfaceMockRecorder struct {
	mock *MockProcessManagerInterface
}

// NewMockProcessManagerInterface creates a new mock instance.
func NewMockProcessManagerInterface(ctrl *gomock.Controller) *MockProcessManagerInterface {
	mock := &MockProcessManagerInterface{ctrl: ctrl}
	mock.recorder = &MockProcessManagerInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockProcessManagerInterface) EXPECT() *MockProcessManagerInterfaceMockRecorder {
	return m.recorder
}

// GetRunningProcess mocks base method.
func (m *MockProcessManagerInterface) GetRunningProcess(process, commandName string) *domain.Process {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRunningProcess", process, commandName)
	ret0, _ := ret[0].(*domain.Process)
	return ret0
}

// GetRunningProcess indicates an expected call of GetRunningProcess.
func (mr *MockProcessManagerInterfaceMockRecorder) GetRunningProcess(process, commandName any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRunningProcess", reflect.TypeOf((*MockProcessManagerInterface)(nil).GetRunningProcess), process, commandName)
}

// GetRunningProcesses mocks base method.
func (m *MockProcessManagerInterface) GetRunningProcesses() map[string]*domain.Process {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRunningProcesses")
	ret0, _ := ret[0].(map[string]*domain.Process)
	return ret0
}

// GetRunningProcesses indicates an expected call of GetRunningProcesses.
func (mr *MockProcessManagerInterfaceMockRecorder) GetRunningProcesses() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRunningProcesses", reflect.TypeOf((*MockProcessManagerInterface)(nil).GetRunningProcesses))
}

// Run mocks base method.
func (m *MockProcessManagerInterface) Run(process, commandName string, command []string, dir string) (*int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Run", process, commandName, command, dir)
	ret0, _ := ret[0].(*int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Run indicates an expected call of Run.
func (mr *MockProcessManagerInterfaceMockRecorder) Run(process, commandName, command, dir any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockProcessManagerInterface)(nil).Run), process, commandName, command, dir)
}

// RunTty mocks base method.
func (m *MockProcessManagerInterface) RunTty(process, comandName string, command []string, dir string) (*int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RunTty", process, comandName, command, dir)
	ret0, _ := ret[0].(*int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RunTty indicates an expected call of RunTty.
func (mr *MockProcessManagerInterfaceMockRecorder) RunTty(process, comandName, command, dir any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RunTty", reflect.TypeOf((*MockProcessManagerInterface)(nil).RunTty), process, comandName, command, dir)
}

// WriteStdin mocks base method.
func (m *MockProcessManagerInterface) WriteStdin(process *domain.Process, data string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WriteStdin", process, data)
	ret0, _ := ret[0].(error)
	return ret0
}

// WriteStdin indicates an expected call of WriteStdin.
func (mr *MockProcessManagerInterfaceMockRecorder) WriteStdin(process, data any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WriteStdin", reflect.TypeOf((*MockProcessManagerInterface)(nil).WriteStdin), process, data)
}

// MockBroadcastChannelInterface is a mock of BroadcastChannelInterface interface.
type MockBroadcastChannelInterface struct {
	ctrl     *gomock.Controller
	recorder *MockBroadcastChannelInterfaceMockRecorder
}

// MockBroadcastChannelInterfaceMockRecorder is the mock recorder for MockBroadcastChannelInterface.
type MockBroadcastChannelInterfaceMockRecorder struct {
	mock *MockBroadcastChannelInterface
}

// NewMockBroadcastChannelInterface creates a new mock instance.
func NewMockBroadcastChannelInterface(ctrl *gomock.Controller) *MockBroadcastChannelInterface {
	mock := &MockBroadcastChannelInterface{ctrl: ctrl}
	mock.recorder = &MockBroadcastChannelInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockBroadcastChannelInterface) EXPECT() *MockBroadcastChannelInterfaceMockRecorder {
	return m.recorder
}

// NewHub mocks base method.
func (m *MockBroadcastChannelInterface) NewHub() *domain.BroadcastChannel {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NewHub")
	ret0, _ := ret[0].(*domain.BroadcastChannel)
	return ret0
}

// NewHub indicates an expected call of NewHub.
func (mr *MockBroadcastChannelInterfaceMockRecorder) NewHub() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NewHub", reflect.TypeOf((*MockBroadcastChannelInterface)(nil).NewHub))
}

// Run mocks base method.
func (m *MockBroadcastChannelInterface) Run() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Run")
}

// Run indicates an expected call of Run.
func (mr *MockBroadcastChannelInterfaceMockRecorder) Run() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockBroadcastChannelInterface)(nil).Run))
}

// MockConsoleManagerInterface is a mock of ConsoleManagerInterface interface.
type MockConsoleManagerInterface struct {
	ctrl     *gomock.Controller
	recorder *MockConsoleManagerInterfaceMockRecorder
}

// MockConsoleManagerInterfaceMockRecorder is the mock recorder for MockConsoleManagerInterface.
type MockConsoleManagerInterfaceMockRecorder struct {
	mock *MockConsoleManagerInterface
}

// NewMockConsoleManagerInterface creates a new mock instance.
func NewMockConsoleManagerInterface(ctrl *gomock.Controller) *MockConsoleManagerInterface {
	mock := &MockConsoleManagerInterface{ctrl: ctrl}
	mock.recorder = &MockConsoleManagerInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockConsoleManagerInterface) EXPECT() *MockConsoleManagerInterfaceMockRecorder {
	return m.recorder
}

// AddConsoleWithChannel mocks base method.
func (m *MockConsoleManagerInterface) AddConsoleWithChannel(consoleId string, consoleType domain.ConsoleType, inputMode string, channel chan string) *domain.Console {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddConsoleWithChannel", consoleId, consoleType, inputMode, channel)
	ret0, _ := ret[0].(*domain.Console)
	return ret0
}

// AddConsoleWithChannel indicates an expected call of AddConsoleWithChannel.
func (mr *MockConsoleManagerInterfaceMockRecorder) AddConsoleWithChannel(consoleId, consoleType, inputMode, channel any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddConsoleWithChannel", reflect.TypeOf((*MockConsoleManagerInterface)(nil).AddConsoleWithChannel), consoleId, consoleType, inputMode, channel)
}

// AddConsoleWithIoReader mocks base method.
func (m *MockConsoleManagerInterface) AddConsoleWithIoReader(consoleId string, consoleType domain.ConsoleType, inputMode string, console io.Reader) *domain.Console {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddConsoleWithIoReader", consoleId, consoleType, inputMode, console)
	ret0, _ := ret[0].(*domain.Console)
	return ret0
}

// AddConsoleWithIoReader indicates an expected call of AddConsoleWithIoReader.
func (mr *MockConsoleManagerInterfaceMockRecorder) AddConsoleWithIoReader(consoleId, consoleType, inputMode, console any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddConsoleWithIoReader", reflect.TypeOf((*MockConsoleManagerInterface)(nil).AddConsoleWithIoReader), consoleId, consoleType, inputMode, console)
}

// DeleteSubscription mocks base method.
func (m *MockConsoleManagerInterface) DeleteSubscription(consoleId string, subscription chan *[]byte) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "DeleteSubscription", consoleId, subscription)
}

// DeleteSubscription indicates an expected call of DeleteSubscription.
func (mr *MockConsoleManagerInterfaceMockRecorder) DeleteSubscription(consoleId, subscription any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteSubscription", reflect.TypeOf((*MockConsoleManagerInterface)(nil).DeleteSubscription), consoleId, subscription)
}

// GetConsoles mocks base method.
func (m *MockConsoleManagerInterface) GetConsoles() map[string]*domain.Console {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetConsoles")
	ret0, _ := ret[0].(map[string]*domain.Console)
	return ret0
}

// GetConsoles indicates an expected call of GetConsoles.
func (mr *MockConsoleManagerInterfaceMockRecorder) GetConsoles() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetConsoles", reflect.TypeOf((*MockConsoleManagerInterface)(nil).GetConsoles))
}

// GetSubscription mocks base method.
func (m *MockConsoleManagerInterface) GetSubscription(consoleId string) chan *[]byte {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSubscription", consoleId)
	ret0, _ := ret[0].(chan *[]byte)
	return ret0
}

// GetSubscription indicates an expected call of GetSubscription.
func (mr *MockConsoleManagerInterfaceMockRecorder) GetSubscription(consoleId any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSubscription", reflect.TypeOf((*MockConsoleManagerInterface)(nil).GetSubscription), consoleId)
}

// MarkExited mocks base method.
func (m *MockConsoleManagerInterface) MarkExited(id string, exitCode int) *domain.Console {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "MarkExited", id, exitCode)
	ret0, _ := ret[0].(*domain.Console)
	return ret0
}

// MarkExited indicates an expected call of MarkExited.
func (mr *MockConsoleManagerInterfaceMockRecorder) MarkExited(id, exitCode any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MarkExited", reflect.TypeOf((*MockConsoleManagerInterface)(nil).MarkExited), id, exitCode)
}

// RemoveConsole mocks base method.
func (m *MockConsoleManagerInterface) RemoveConsole(consoleId string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoveConsole", consoleId)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemoveConsole indicates an expected call of RemoveConsole.
func (mr *MockConsoleManagerInterfaceMockRecorder) RemoveConsole(consoleId any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveConsole", reflect.TypeOf((*MockConsoleManagerInterface)(nil).RemoveConsole), consoleId)
}

// MockProcessMonitorInterface is a mock of ProcessMonitorInterface interface.
type MockProcessMonitorInterface struct {
	ctrl     *gomock.Controller
	recorder *MockProcessMonitorInterfaceMockRecorder
}

// MockProcessMonitorInterfaceMockRecorder is the mock recorder for MockProcessMonitorInterface.
type MockProcessMonitorInterfaceMockRecorder struct {
	mock *MockProcessMonitorInterface
}

// NewMockProcessMonitorInterface creates a new mock instance.
func NewMockProcessMonitorInterface(ctrl *gomock.Controller) *MockProcessMonitorInterface {
	mock := &MockProcessMonitorInterface{ctrl: ctrl}
	mock.recorder = &MockProcessMonitorInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockProcessMonitorInterface) EXPECT() *MockProcessMonitorInterfaceMockRecorder {
	return m.recorder
}

// AddProcess mocks base method.
func (m *MockProcessMonitorInterface) AddProcess(pid int32, name string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddProcess", pid, name)
}

// AddProcess indicates an expected call of AddProcess.
func (mr *MockProcessMonitorInterfaceMockRecorder) AddProcess(pid, name any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddProcess", reflect.TypeOf((*MockProcessMonitorInterface)(nil).AddProcess), pid, name)
}

// GetAllProcessesMetrics mocks base method.
func (m *MockProcessMonitorInterface) GetAllProcessesMetrics() map[string]*domain.ProcessMonitorMetrics {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllProcessesMetrics")
	ret0, _ := ret[0].(map[string]*domain.ProcessMonitorMetrics)
	return ret0
}

// GetAllProcessesMetrics indicates an expected call of GetAllProcessesMetrics.
func (mr *MockProcessMonitorInterfaceMockRecorder) GetAllProcessesMetrics() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllProcessesMetrics", reflect.TypeOf((*MockProcessMonitorInterface)(nil).GetAllProcessesMetrics))
}

// GetPsTrees mocks base method.
func (m *MockProcessMonitorInterface) GetPsTrees() map[string]*domain.ProcessTreeRoot {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPsTrees")
	ret0, _ := ret[0].(map[string]*domain.ProcessTreeRoot)
	return ret0
}

// GetPsTrees indicates an expected call of GetPsTrees.
func (mr *MockProcessMonitorInterfaceMockRecorder) GetPsTrees() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPsTrees", reflect.TypeOf((*MockProcessMonitorInterface)(nil).GetPsTrees))
}

// RemoveProcess mocks base method.
func (m *MockProcessMonitorInterface) RemoveProcess(name string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RemoveProcess", name)
}

// RemoveProcess indicates an expected call of RemoveProcess.
func (mr *MockProcessMonitorInterfaceMockRecorder) RemoveProcess(name any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveProcess", reflect.TypeOf((*MockProcessMonitorInterface)(nil).RemoveProcess), name)
}

// MockTemplateRendererInterface is a mock of TemplateRendererInterface interface.
type MockTemplateRendererInterface struct {
	ctrl     *gomock.Controller
	recorder *MockTemplateRendererInterfaceMockRecorder
}

// MockTemplateRendererInterfaceMockRecorder is the mock recorder for MockTemplateRendererInterface.
type MockTemplateRendererInterfaceMockRecorder struct {
	mock *MockTemplateRendererInterface
}

// NewMockTemplateRendererInterface creates a new mock instance.
func NewMockTemplateRendererInterface(ctrl *gomock.Controller) *MockTemplateRendererInterface {
	mock := &MockTemplateRendererInterface{ctrl: ctrl}
	mock.recorder = &MockTemplateRendererInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTemplateRendererInterface) EXPECT() *MockTemplateRendererInterfaceMockRecorder {
	return m.recorder
}

// RenderScrollTemplateFiles mocks base method.
func (m *MockTemplateRendererInterface) RenderScrollTemplateFiles(templateFiles []string, data any, ouputPath string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RenderScrollTemplateFiles", templateFiles, data, ouputPath)
	ret0, _ := ret[0].(error)
	return ret0
}

// RenderScrollTemplateFiles indicates an expected call of RenderScrollTemplateFiles.
func (mr *MockTemplateRendererInterfaceMockRecorder) RenderScrollTemplateFiles(templateFiles, data, ouputPath any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RenderScrollTemplateFiles", reflect.TypeOf((*MockTemplateRendererInterface)(nil).RenderScrollTemplateFiles), templateFiles, data, ouputPath)
}

// RenderTemplate mocks base method.
func (m *MockTemplateRendererInterface) RenderTemplate(templatePath string, data any) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RenderTemplate", templatePath, data)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RenderTemplate indicates an expected call of RenderTemplate.
func (mr *MockTemplateRendererInterfaceMockRecorder) RenderTemplate(templatePath, data any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RenderTemplate", reflect.TypeOf((*MockTemplateRendererInterface)(nil).RenderTemplate), templatePath, data)
}

// MockOciRegistryInterface is a mock of OciRegistryInterface interface.
type MockOciRegistryInterface struct {
	ctrl     *gomock.Controller
	recorder *MockOciRegistryInterfaceMockRecorder
}

// MockOciRegistryInterfaceMockRecorder is the mock recorder for MockOciRegistryInterface.
type MockOciRegistryInterfaceMockRecorder struct {
	mock *MockOciRegistryInterface
}

// NewMockOciRegistryInterface creates a new mock instance.
func NewMockOciRegistryInterface(ctrl *gomock.Controller) *MockOciRegistryInterface {
	mock := &MockOciRegistryInterface{ctrl: ctrl}
	mock.recorder = &MockOciRegistryInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockOciRegistryInterface) EXPECT() *MockOciRegistryInterfaceMockRecorder {
	return m.recorder
}

// CanUpdateTag mocks base method.
func (m *MockOciRegistryInterface) CanUpdateTag(descriptor v1.Descriptor, folder, tag string) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CanUpdateTag", descriptor, folder, tag)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CanUpdateTag indicates an expected call of CanUpdateTag.
func (mr *MockOciRegistryInterfaceMockRecorder) CanUpdateTag(descriptor, folder, tag any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CanUpdateTag", reflect.TypeOf((*MockOciRegistryInterface)(nil).CanUpdateTag), descriptor, folder, tag)
}

// CreateMetaDescriptors mocks base method.
func (m *MockOciRegistryInterface) CreateMetaDescriptors(fs *file.Store, dir, artifact string) (v1.Descriptor, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateMetaDescriptors", fs, dir, artifact)
	ret0, _ := ret[0].(v1.Descriptor)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateMetaDescriptors indicates an expected call of CreateMetaDescriptors.
func (mr *MockOciRegistryInterfaceMockRecorder) CreateMetaDescriptors(fs, dir, artifact any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateMetaDescriptors", reflect.TypeOf((*MockOciRegistryInterface)(nil).CreateMetaDescriptors), fs, dir, artifact)
}

// GetRepo mocks base method.
func (m *MockOciRegistryInterface) GetRepo(repoUrl string) (*remote.Repository, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRepo", repoUrl)
	ret0, _ := ret[0].(*remote.Repository)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetRepo indicates an expected call of GetRepo.
func (mr *MockOciRegistryInterfaceMockRecorder) GetRepo(repoUrl any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRepo", reflect.TypeOf((*MockOciRegistryInterface)(nil).GetRepo), repoUrl)
}

// PackFolders mocks base method.
func (m *MockOciRegistryInterface) PackFolders(fs *file.Store, dirs []string, artifactType domain.ArtifactType, path string) (v1.Descriptor, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PackFolders", fs, dirs, artifactType, path)
	ret0, _ := ret[0].(v1.Descriptor)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PackFolders indicates an expected call of PackFolders.
func (mr *MockOciRegistryInterfaceMockRecorder) PackFolders(fs, dirs, artifactType, path any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PackFolders", reflect.TypeOf((*MockOciRegistryInterface)(nil).PackFolders), fs, dirs, artifactType, path)
}

// Pull mocks base method.
func (m *MockOciRegistryInterface) Pull(dir, artifact string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Pull", dir, artifact)
	ret0, _ := ret[0].(error)
	return ret0
}

// Pull indicates an expected call of Pull.
func (mr *MockOciRegistryInterfaceMockRecorder) Pull(dir, artifact any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Pull", reflect.TypeOf((*MockOciRegistryInterface)(nil).Pull), dir, artifact)
}

// Push mocks base method.
func (m *MockOciRegistryInterface) Push(folder, repo, tag string, annotationInfo domain.AnnotationInfo, packMeta bool) (v1.Descriptor, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Push", folder, repo, tag, annotationInfo, packMeta)
	ret0, _ := ret[0].(v1.Descriptor)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Push indicates an expected call of Push.
func (mr *MockOciRegistryInterfaceMockRecorder) Push(folder, repo, tag, annotationInfo, packMeta any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Push", reflect.TypeOf((*MockOciRegistryInterface)(nil).Push), folder, repo, tag, annotationInfo, packMeta)
}

// PushMeta mocks base method.
func (m *MockOciRegistryInterface) PushMeta(folder, repo string) (v1.Descriptor, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PushMeta", folder, repo)
	ret0, _ := ret[0].(v1.Descriptor)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PushMeta indicates an expected call of PushMeta.
func (mr *MockOciRegistryInterfaceMockRecorder) PushMeta(folder, repo any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PushMeta", reflect.TypeOf((*MockOciRegistryInterface)(nil).PushMeta), folder, repo)
}
