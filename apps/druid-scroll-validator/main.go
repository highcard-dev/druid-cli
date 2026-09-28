// druid-scroll-validator validates bounded YAML through the runtime's semantic
// rules. It never expands environment variables, reads configuration, unpacks
// artifacts, contacts a daemon, or executes Scroll commands.
package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"gopkg.in/yaml.v2"
)

const maxInputBytes = 4 * 1024 * 1024

func validate(input io.Reader, output io.Writer) {
	data, err := io.ReadAll(io.LimitReader(input, maxInputBytes+1))
	scroll := &domain.Scroll{}
	valid := err == nil && len(data) <= maxInputBytes && yaml.Unmarshal(data, &scroll.File) == nil && scroll.Validate(false) == nil
	// Do not echo untrusted content or host details in validation errors.
	_ = json.NewEncoder(output).Encode(struct {
		Version int  `json:"version"`
		Valid   bool `json:"valid"`
	}{Version: 1, Valid: valid})
}

func main() { validate(os.Stdin, os.Stdout) }
