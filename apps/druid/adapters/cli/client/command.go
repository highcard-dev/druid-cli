package client

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/spf13/cobra"
)

var CommandCommand = &cobra.Command{
	Use:   "command",
	Short: "Inspect and run scroll commands",
}

var CommandRunCommand = &cobra.Command{
	Use:   "run <name> <command>",
	Short: "Run a command on a daemon-managed scroll",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		daemon, err := runtimeDaemonClient()
		if err != nil {
			return err
		}
		scroll, err := daemon.RunScrollCommand(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		return printJSON(scroll)
	},
}

var CommandListCommand = &cobra.Command{
	Use:   "list <name>",
	Short: "List commands for a daemon-managed scroll",
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
		queue, err := daemon.GetScrollQueue(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printCommandRows(commandRows(file, queue))
	},
}

type commandRow struct {
	command    string
	status     string
	runMode    string
	procedures int
}

func commandRows(file *domain.File, queue map[string]domain.ScrollLockStatus) []commandRow {
	if file == nil {
		return nil
	}
	commands := make([]string, 0, len(file.Commands))
	for command := range file.Commands {
		commands = append(commands, command)
	}
	sort.Strings(commands)

	rows := []commandRow{}
	for _, command := range commands {
		definition := file.Commands[command]
		if definition == nil {
			continue
		}
		status := string(queue[command])
		if status == "" {
			status = "-"
		}
		runMode := string(definition.Run)
		if runMode == "" {
			runMode = string(domain.RunModeAlways)
		}
		rows = append(rows, commandRow{
			command:    command,
			status:     status,
			runMode:    runMode,
			procedures: len(definition.Procedures),
		})
	}
	return rows
}

func printCommandRows(rows []commandRow) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMMAND\tSTATUS\tRUN\tPROCEDURES")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", row.command, row.status, row.runMode, row.procedures)
	}
	return w.Flush()
}
