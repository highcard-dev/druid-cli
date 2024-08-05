package services

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"github.com/shirou/gopsutil/net"
	"go.uber.org/zap"
)

type PortMonitor struct {
	ports            []*domain.AugmentedPort
	portPoolInterval time.Duration
}

func NewPortServiceWithScrollFile(
	file *domain.File,
) *PortMonitor {
	p := &PortMonitor{
		portPoolInterval: 5 * time.Second,
	}
	p.SyncPortEnv(file)
	return p
}

func NewPortService(ports []int) *PortMonitor {
	ap := make([]*domain.AugmentedPort, len(ports))

	for idx, port := range ports {
		ap[idx] = &domain.AugmentedPort{
			Port: domain.Port{
				Name: fmt.Sprintf("port%d", port),
				Port: port,
			},
		}
	}

	p := &PortMonitor{
		ports:            ap,
		portPoolInterval: 5 * time.Second,
	}
	return p
}

func (p *PortMonitor) SyncPortEnv(file *domain.File) []*domain.AugmentedPort {
	ports := file.Ports

	augmentedPorts := make([]*domain.AugmentedPort, len(ports))

	for idx, port := range ports {
		portEnvName := fmt.Sprintf("DRUID_PORT_%s", strings.ToUpper(port.Name))
		envPort := os.Getenv(portEnvName)

		if envPort != "" {
			portInt, err := strconv.Atoi(envPort)
			if err != nil {
				port.Port = portInt
			}
		}

		ap := &domain.AugmentedPort{
			Port: port,
		}

		augmentedPorts[idx] = ap
		os.Setenv(portEnvName, strconv.Itoa(port.Port))
	}

	p.ports = augmentedPorts
	return p.ports
}

func (p *PortMonitor) GetLastActivity(port int) uint {
	for _, p := range p.ports {
		if p.Port.Port == port {
			return uint(time.Since(p.InactiveSince).Seconds())
		}
	}

	return 0
}

func (po *PortMonitor) GetPorts() []*domain.AugmentedPort {
	for _, p := range po.ports {
		p.Open = po.CheckOpen(p.Port.Port)

		inactiveCorrected := time.Since(p.InactiveSince) - po.portPoolInterval
		if inactiveCorrected < 0 {
			p.InactiveSinceSec = 0
		} else {
			p.InactiveSinceSec = uint(inactiveCorrected.Seconds())
		}
	}

	return po.ports
}

func (p *PortMonitor) CheckOpen(port int) bool {
	//check if port is open

	connections, err := net.Connections("inet")
	if err != nil {
		return false
	}

	for _, conn := range connections {
		if conn.Laddr.Port == uint32(port) {
			return true
		}
	}
	return false
}

func (p *PortMonitor) WaitForConnection(ifaces []string) {

	for {
		ports := make([]int, len(p.ports))
		for idx, port := range p.ports {
			ports[idx] = port.Port.Port
		}

		firstOnlinePort, err := p.StartMonitorPorts(ports, ifaces, 5*time.Minute)

		if err != nil {
			logger.Log().Error("Error on port monitoring", zap.Error(err))
		} else {
			if firstOnlinePort == nil {
				break
			}

			for _, port := range p.ports {
				//this is not right but sufficient for now, later we should only update one port
				port.InactiveSince = time.Now()
			}
		}

		time.Sleep(p.portPoolInterval)
	}
}

func (p *PortMonitor) StartMonitoring(ctx context.Context, ifaces []string) {
	//start monitoring the ports
	for {
		select {
		case <-ctx.Done():
			return
		default:
			p.WaitForConnection(ifaces)
		}
	}
}

func (p *PortMonitor) StartMonitorPorts(ports []int, ifaces []string, timeout time.Duration) (*int, error) {

	// Find all network interfaces
	interfaces, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}

	var ifacesFound []string
	for _, iface := range interfaces {
		if slices.Contains(ifaces, iface.Name) {
			ifacesFound = append(ifacesFound, iface.Name)
			continue
		}
	}

	logger.Log().Debug("Found interfaces", zap.Strings("ifaces", ifacesFound), zap.Strings("requestedInterfaces", ifaces))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	var doneIface string
	var donePort int

	for _, iface := range ifacesFound {
		go func(po []int, i string) {
			port, err := p.waitForPortActiviy(ctx, ports, i)
			if err != nil {
				logger.Log().Error("Error on port monitoring", zap.String("iface", i), zap.Ints("ports", po), zap.Error(err))
				return
			}

			if port == 0 {
				return
			}
			donePort = port
			doneIface = i
			cancel()
		}(ports, iface)
	}

	<-ctx.Done()

	//this is not needed, but it's a good practice to call it
	cancel()

	if doneIface != "" {
		logger.Log().Debug("Port activity found", zap.String("iface", doneIface), zap.Int("port", donePort))
		return &donePort, nil
	} else {
		logger.Log().Debug("No port activity found on any interface\n")
		return nil, nil
	}

}

func (p *PortMonitor) waitForPortActiviy(ctx context.Context, ports []int, interfaceName string) (int, error) {

	handle, err := pcap.OpenLive(interfaceName, 1600, true, pcap.BlockForever)
	if err != nil {
		return 0, err
	}

	go func() {
		<-ctx.Done()
		logger.Log().Debug("Closing handle ", zap.String("iface", interfaceName), zap.Ints("ports", ports))
		handle.Close()
	}()

	portFilterParts := make([]string, len(ports))

	for idx, port := range ports {
		portFilterParts[idx] = fmt.Sprintf("port %d", port)
	}

	filter := strings.Join(portFilterParts, " or ")

	err = handle.SetBPFFilter(filter)
	if err != nil {
		return 0, err
	}
	logger.Log().Debug("Listening on iface", zap.String("iface", interfaceName), zap.Ints("ports", ports))

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	for packet := range packetSource.Packets() {
		// Process the packet here
		if packet.ApplicationLayer() == nil {
			continue
		} else {
			packetPort := packet.TransportLayer().TransportFlow().Dst().String()
			packetPortInt, err := strconv.Atoi(packetPort)
			if err != nil {
				packetPortInt = 0
			}
			logger.Log().Debug("Packet found on iface", zap.String("iface", interfaceName), zap.Int("port", packetPortInt))
			return packetPortInt, nil
		}
	}
	return 0, nil
}