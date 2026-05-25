package client

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/highcard-dev/daemon/internal/api"
)

func printScrolls(scrolls []api.RuntimeScroll) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tSCROLL")
	for _, scroll := range scrolls {
		fmt.Fprintf(w, "%s\t%s\t%s\n", scroll.Id, scroll.Status, scroll.ScrollName)
	}
	return w.Flush()
}

func printJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
