package services

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	processutil "github.com/shirou/gopsutil/process"
	"go.uber.org/zap"
)

var ErrorProcessNotActive = fmt.Errorf("process not active")

type ProcessMonitor struct {
	exportedMetrics *ProcessMonitorMetricsExported
	processes       map[string]*processutil.Process
	mu              sync.Mutex
}

type ProcessMonitorMetricsExported struct {
	prometheusCpuUsage        *prometheus.GaugeVec
	prometheusMemoryUsage     *prometheus.GaugeVec
	prometheusConnectionCount *prometheus.GaugeVec
}

func NewProcessMonitor(enableMetrics bool) *ProcessMonitor {

	pm := &ProcessMonitor{
		processes: make(map[string]*processutil.Process),
	}

	if enableMetrics {
		pm.exportedMetrics = NewProcessMonitorMetricsExported()
	}

	return pm
}

func NewProcessMonitorMetricsExported() *ProcessMonitorMetricsExported {
	return &ProcessMonitorMetricsExported{
		prometheusCpuUsage: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: "druid",
			Name:      "cpu1",
			Help:      "CPU usage",
		}, []string{"process"}),
		prometheusMemoryUsage: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "druid",
			Name:      "memory",
			Help:      "Memory usage",
		}, []string{"process"}),
		prometheusConnectionCount: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "druid",
			Name:      "connections",
			Help:      "Connections",
		}, []string{"process"}),
	}
}

func (po *ProcessMonitor) ShutdownPromMetrics() {
	if po.exportedMetrics == nil {

		logger.Log().Warn("No metrics registered, skipping")
		return
	}
	logger.Log().Info("Shutting down prometheus metrics")
	prometheus.DefaultRegisterer.Unregister(po.exportedMetrics.prometheusCpuUsage)
	prometheus.DefaultRegisterer.Unregister(po.exportedMetrics.prometheusMemoryUsage)
	prometheus.DefaultRegisterer.Unregister(po.exportedMetrics.prometheusConnectionCount)
}

func (po *ProcessMonitor) StartMonitoring() {
	ticker := time.NewTicker(time.Second)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				po.RefreshMetrics()
			}
		}
	}()
}

func (po *ProcessMonitor) RefreshMetrics() {
	po.mu.Lock()
	defer po.mu.Unlock()
	for name, process := range po.processes {

		_, err := po.GetProcessMetric(name, process)
		if err != nil {
			logger.Log().Error("Error when retrieving process Metrics",
				zap.String(logger.LogKeyContext, logger.LogContextMonitor),
				zap.String("processName", name),
				zap.Error(err),
			)
		}
	}
}

func (po *ProcessMonitor) GetProcessMetric(name string, p *processutil.Process) (*domain.ProcessMonitorMetrics, error) {

	running, err := p.IsRunning()
	if err != nil {
		return nil, err
	}
	if running {
		memory, cpu, cons := calcUsageOfProcess(p, true)

		if po.exportedMetrics != nil {
			po.exportedMetrics.prometheusCpuUsage.With(prometheus.Labels{"process": name}).Set(cpu)
			po.exportedMetrics.prometheusMemoryUsage.With(prometheus.Labels{"process": name}).Set(float64(memory))
			po.exportedMetrics.prometheusConnectionCount.With(prometheus.Labels{"process": name}).Set(float64(len(cons)))
		}

		return &domain.ProcessMonitorMetrics{
			Cpu:         cpu,
			Memory:      memory,
			Connections: cons,
			Pid:         int(p.Pid),
		}, nil
	} else {

		return nil, ErrorProcessNotActive
	}
}

func (po *ProcessMonitor) AddProcess(pid int32, name string) {
	process, err := processutil.NewProcess(pid)
	if err != nil {
		logger.Log().Error("Error when adding process",
			zap.String(logger.LogKeyContext, logger.LogContextMonitor),
			zap.Int32("pid", pid),
			zap.Error(err),
		)
		return
	}
	po.mu.Lock()
	defer po.mu.Unlock()

	po.processes[name] = process
}

func (po *ProcessMonitor) RemoveProcess(name string) {
	po.mu.Lock()
	defer po.mu.Unlock()

	delete(po.processes, name)
}

func calcUsageOfProcess(p *processutil.Process, excludePrivateIP bool) (int, float64, []string) {
	if b, err := p.IsRunning(); !b || err != nil {
		return 0, 0, []string{}
	}

	memory, _ := p.MemoryInfo()
	cpu1, _ := p.CPUPercent()
	//	cpu2, _ := p.CPUAffinity()
	connections, _ := p.Connections()
	var memoryNum int

	if memory != nil {
		memoryNum = int(memory.RSS)
	} else {
		memoryNum = 0
	}

	children, _ := p.Children()

	var cons = []string{}
	for _, con := range connections {

		if excludePrivateIP && isPrivateIP(net.ParseIP(con.Raddr.IP)) {
			continue
		}
		if con.Raddr.IP == "" || con.Raddr.Port == 0 {
			continue
		}

		cons = append(cons, con.Raddr.IP+":"+fmt.Sprint(con.Raddr.Port))
	}
	//recursivly fetch process tree
	for _, cp := range children {
		cmem, ccpu, ccons := calcUsageOfProcess(cp, true)
		memoryNum += cmem
		cpu1 += ccpu
		cons = append(cons, ccons...)
	}

	return memoryNum, cpu1, cons
}

func (p *ProcessMonitor) GetAllProcessesMetrics() map[string]*domain.ProcessMonitorMetrics {

	metrics := make(map[string]*domain.ProcessMonitorMetrics)

	for key, process := range p.processes {
		m, _ := p.GetProcessMetric(key, process)
		metrics[key] = m
	}
	return metrics
}

func (p *ProcessMonitor) GetPsTrees() map[string]*domain.ProcessTreeRoot {

	trees := make(map[string]*domain.ProcessTreeRoot)

	for key, process := range p.processes {
		tree := GetTree(process)
		trees[key] = tree
	}

	return trees

}

var privateIPBlocks []*net.IPNet

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // RFC3927 link-local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local addr
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Errorf("parse error on %q: %v", cidr, err))
		}
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

func GetTree(p *processutil.Process) *domain.ProcessTreeRoot {
	tree := &domain.ProcessTreeRoot{
		Root: &domain.ProcessTreeNode{},
	}
	GetTreeRec(p, tree, tree.Root)
	return tree
}

func GetTreeRec(process *processutil.Process, tree *domain.ProcessTreeRoot, current *domain.ProcessTreeNode) {
	current.Process = process
	current.CpuPercent, _ = process.CPUPercent()
	current.Memory, _ = process.MemoryInfo()
	current.MemoryEx, _ = process.MemoryInfoEx()
	current.IOCounters, _ = process.IOCounters()
	current.Name, _ = process.Name()
	current.Cmdline, _ = process.Cmdline()
	current.Gids, _ = process.Gids()
	current.Username, _ = process.Username()

	tree.TotalCpuPercent += current.CpuPercent
	if current.Memory != nil {
		tree.TotalMemoryRss += current.Memory.RSS
		tree.TotalMemoryVms += current.Memory.VMS
		tree.TotalMemorySwap += current.Memory.Swap
	}
	if current.IOCounters != nil {
		tree.TotalIoCountersRead += current.IOCounters.ReadCount
		tree.TotalIoCountersWrite += current.IOCounters.WriteCount
	}

	var childs []*domain.ProcessTreeNode
	children, err := process.Children()
	if err != nil {
		return
	}
	for _, child := range children {
		childTree := &domain.ProcessTreeNode{}
		GetTreeRec(child, tree, childTree)
		childs = append(childs, childTree)
	}

	current.Children = childs
}
