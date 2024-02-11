package domain

import (
	"container/list"
	"errors"
	"io"
	"os"
	"os/exec"

	processutil "github.com/shirou/gopsutil/process"
)

type Process struct {
	Cmd   *exec.Cmd
	Name  string
	Type  string
	StdIn io.WriteCloser
}

type Log struct {
	List     *list.List
	Capacity uint
	Req      chan chan<- []byte
	Write    chan<- []byte
}

type ProcessMonitorMetrics struct {
	Cpu         float64
	Memory      int
	Connections []string
	Pid         int
} // @name ProcessMonitorMetrics

func (process *Process) Stop() error {
	if process.Cmd == nil {
		return nil
	}
	//TODO: stop process
	println("TODO: stop process")
	//process.Cmd = nil
	return nil
}

func (process *Process) Status() *os.Process {
	return process.Cmd.Process
}

type ProcessTreeRoot struct {
	Root                 *ProcessTreeNode `json:"root"`
	TotalMemoryRss       uint64           `json:"total_memory_rss"`
	TotalMemoryVms       uint64           `json:"total_memory_vms"`
	TotalMemorySwap      uint64           `json:"total_memory_swap"`
	TotalIoCountersRead  uint64           `json:"total_io_counters_read"`
	TotalIoCountersWrite uint64           `json:"total_io_counters_write"`
	TotalCpuPercent      float64          `json:"total_cpu_percent"`
	TotalProcessCount    uint             `json:"total_process_count"`
} // @name ProcessTreeRoot

type ProcessTreeNode struct {
	Process    *processutil.Process          `json:"process"`
	Memory     *processutil.MemoryInfoStat   `json:"memory"`
	MemoryEx   *processutil.MemoryInfoExStat `json:"memory_ex"`
	IOCounters *processutil.IOCountersStat   `json:"io_counters"`
	CpuPercent float64                       `json:"cpu_percent"`
	Name       string                        `json:"name"`
	Gids       []int32                       `json:"gids"`
	Username   string                        `json:"username"`
	Cmdline    string                        `json:"cmdline"`
	Children   []*ProcessTreeNode            `json:"children"`
} // @name ProcessTreeNode

func (process *Process) GetProcess() (*processutil.Process, error) {

	var status = process.Cmd.Process
	if status == nil || status.Pid < 0 {
		return nil, errors.New("process not initialized")
	}
	exists, _ := processutil.PidExists(int32(status.Pid))
	if !exists {
		process.Stop()
		return nil, errors.New("process not running")
	}
	return processutil.NewProcess(int32(status.Pid))
}
