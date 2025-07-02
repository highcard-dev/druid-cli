package services

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	pcap "github.com/packetcap/go-pcap"
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
			InactiveSince:    time.Now(),
			InactiveSinceSec: 0,
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

	var augmentedPorts []*domain.AugmentedPort

	for _, port := range ports {

		//TODO: get rid of this and set this directly in scroll.yaml, when templating is implemented
		portEnvName := fmt.Sprintf("DRUID_PORT_%s", strings.ToUpper(port.Name))
		envPort := os.Getenv(portEnvName)

		if envPort != "" && port.Port == 0 {
			portInt, err := strconv.Atoi(envPort)
			if err == nil {
				port.Port = portInt
			}
		}

		if port.Port == 0 {
			logger.Log().Warn("Could no find port number for port", zap.String("port", port.Name))
			continue
		}

		augmentedPorts = append(augmentedPorts, &domain.AugmentedPort{
			Port:          port,
			InactiveSince: time.Now(),
		})
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

func (p *PortMonitor) ResetOpenPorts() {
	for _, p := range p.ports {
		p.InactiveSince = time.Now()
	}
}

func (p *PortMonitor) GetPort(port int) *domain.AugmentedPort {
	for _, p := range p.ports {
		if p.Port.Port == port {
			return p
		}
	}
	return nil
}

func (p *PortMonitor) MandatoryPortsOpen() bool {
	augmentedPorts := p.GetPorts()

	for _, port := range augmentedPorts {
		if port.Mandatory && !port.Open {
			logger.Log().Warn("Mandatory port not open", zap.String("port", port.Port.Name), zap.Int("portnum", port.Port.Port))
			return false
		}
	}
	return true
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

func (p *PortMonitor) WaitForConnection(ifaces []string, ppm uint) error {

	for {
		var ports []int
		for _, port := range p.ports {
			if port.Port.CheckActivity {
				ports = append(ports, port.Port.Port)
			}
		}

		if len(ports) == 0 {
			return fmt.Errorf("no ports to monitor")
		}

		firstOnlinePort := p.StartMonitorPorts(ports, ifaces, 5*time.Minute, ppm)

		if firstOnlinePort == nil {
			continue
		}

		for _, port := range p.ports {
			//this is not right but sufficient for now, later we should only update one port
			port.InactiveSince = time.Now()
		}

		time.Sleep(p.portPoolInterval)
	}
}

func (p *PortMonitor) StartMonitoring(ctx context.Context, ifaces []string, ppm uint) {
	//start monitoring the ports
	for {
		select {
		case <-ctx.Done():
			return
		default:
			err := p.WaitForConnection(ifaces, ppm)
			if err != nil {
				logger.Log().Error("Error while waiting for connection", zap.Error(err))
				return
			}
		}
	}
}

func (p *PortMonitor) StartMonitorPorts(ports []int, ifaces []string, timeout time.Duration, ppm uint) *int {

	// Find all network interfaces

	logger.Log().Debug("Found interfaces", zap.Strings("ifaces", ifaces), zap.Strings("requestedInterfaces", ifaces))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	var doneIface string
	var donePort int

	for _, iface := range ifaces {
		go func(po []int, i string) {
			port, err := p.waitForPortActiviy(ctx, ports, i, ppm)
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
		return &donePort
	} else {
		logger.Log().Debug("No port activity found on any interface\n")
		return nil
	}

}
func (p *PortMonitor) waitForPortActiviy(ctx context.Context, ports []int, interfaceName string, ppm uint) (int, error) {

	handle, err := pcap.OpenLive(interfaceName, 1600, true, time.Hour, false)
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

	lt1 := layers.LinkType(handle.LinkType())
	packetSource := gopacket.NewPacketSource(handle, lt1)

	// Introduce a ticker to reset packet count every minute
	packetCount := 0
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, nil
		case packet := <-packetSource.Packets():
			if packet == nil {
				continue
			}

			// Process the packet and check if it has an application layer
			if packet.ApplicationLayer() == nil {
				continue
			}

			var packetPort = 0

			if transportLayer := packet.TransportLayer(); transportLayer != nil {
				packetPortStr := transportLayer.TransportFlow().Dst().String()
				packetPort, err = strconv.Atoi(packetPortStr)
				if err != nil {
					packetPort = 0
				}
			}

			var srcIP, dstIP string
			if netLayer := packet.NetworkLayer(); netLayer != nil {
				srcIP = netLayer.NetworkFlow().Src().String()
				dstIP = netLayer.NetworkFlow().Dst().String()
			}

			logger.Log().Debug("Packet found on iface",
				zap.String("iface", interfaceName), zap.Int("port", packetPort),
				zap.String("srcIP", srcIP), zap.String("dstIP", dstIP),
			)

			// Increment packet count
			packetCount++

			// Check if we have reached the packets per minute threshold
			if packetCount >= int(ppm) {
				logger.Log().Info("PPM threshhold reached", zap.String("iface", interfaceName), zap.Int("ppm", int(ppm)))
				return packetPort, nil
			}
		case <-ticker.C:
			// Reset packet count every minute
			packetCount = 0
		}
	}
}
