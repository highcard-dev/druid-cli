package cmd

import (
	"fmt"
	"strconv"
	"time"

	"github.com/highcard-dev/daemon/internal/core/services"
	"github.com/spf13/cobra"
)

var PortMonitorCmd = &cobra.Command{
	Use:   "port",
	Short: "Monitor ports",
	Args:  cobra.MinimumNArgs(1),
	Long:  "Utility to monitor ports and show their status and activity",
	RunE: func(cmd *cobra.Command, args []string) error {

		ports := make([]int, len(args))

		for idx, port := range args {
			i, err := strconv.Atoi(port)
			if err != nil {
				return err
			}
			ports[idx] = i
		}

		portMonitor := services.NewPortService(ports)

		go portMonitor.StartMonitoring(cmd.Context(), watchPortsInterfaces)

		for {
			ps := portMonitor.GetPorts()
			for _, p := range ps {
				fmt.Printf("Port %s: %d, last activity %v, open: %t \n", p.Port.Name, p.Port.Port, p.InactiveSince, p.Open)
			}
			time.Sleep(5 * time.Second)
		}

	},
}

func init() {
	PortMonitorCmd.Flags().StringArrayVarP(&watchPortsInterfaces, "watch-ports-interfaces", "", []string{"lo0"}, "Interfaces to watch for port activity")
}
