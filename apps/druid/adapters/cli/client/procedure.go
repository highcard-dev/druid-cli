package client

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/spf13/cobra"
)

var ProcedureCommand = &cobra.Command{
	Use:   "procedure",
	Short: "Inspect and attach to runtime procedures",
}

var ProcedureListCommand = &cobra.Command{
	Use:   "list <name>",
	Short: "List procedures for a daemon-managed scroll",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		file, err := daemon.GetScrollConfig(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		statuses, err := daemon.GetScrollQueue(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		consoles, err := daemon.GetScrollConsoles(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printProcedureRows(procedureRows(file, statuses, consoles))
	},
}

var ProcedureAttachCommand = &cobra.Command{
	Use:   "attach <name> <procedure>",
	Short: "Attach to a running procedure console",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		consoles, err := daemon.GetScrollConsoles(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if _, ok := consoles[args[1]]; !ok {
			return fmt.Errorf("procedure console %q is not active; run `druid procedure list %s`", args[1], args[0])
		}
		if config.AttachConsole == nil {
			return fmt.Errorf("procedure attach is not configured")
		}
		return config.AttachConsole(cmd.Context(), args[0], args[1])
	},
}

type procedureRow struct {
	command   string
	procedure string
	status    string
	console   string
}

func procedureRows(file *domain.File, statuses domain.ProcedureStatusMap, consoles map[string]domain.Console) []procedureRow {
	if file == nil {
		return nil
	}
	commands := make([]string, 0, len(file.Commands))
	for command := range file.Commands {
		commands = append(commands, command)
	}
	sort.Strings(commands)

	rows := []procedureRow{}
	for _, command := range commands {
		definition := file.Commands[command]
		if definition == nil {
			continue
		}
		for idx, procedure := range definition.Procedures {
			name := domain.ProcedureName(command, idx, procedure)
			status := procedureStatus(name, statuses[command][name], consoles)
			console := "no"
			if _, ok := consoles[name]; ok {
				console = "yes"
			}
			rows = append(rows, procedureRow{command: command, procedure: name, status: status, console: console})
		}
	}
	return rows
}

func procedureStatus(name string, status domain.LockStatus, consoles map[string]domain.Console) string {
	if status.Status != "" {
		return string(status.Status)
	}
	if console, ok := consoles[name]; ok {
		if console.Exit == nil {
			return string(domain.ScrollLockStatusRunning)
		}
		if *console.Exit == 0 {
			return string(domain.ScrollLockStatusDone)
		}
		return string(domain.ScrollLockStatusError)
	}
	return "-"
}

func printProcedureRows(rows []procedureRow) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMMAND\tPROCEDURE\tSTATUS\tCONSOLE")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.command, row.procedure, row.status, row.console)
	}
	return w.Flush()
}
