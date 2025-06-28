package test_utils

import (
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/handler"
)

func WaitForConsoleRunning(console string, duration time.Duration) error {

	timeout := time.After(duration)

	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-timeout:
			return errors.New("timeout waiting for console to start")
		case <-ticker.C:
			body, err := FetchBytes("http://localhost:8081/api/v1/consoles")
			if err != nil {
				continue
			}

			var resp handler.ConsolesHttpResponse

			json.Unmarshal(body, &resp)

			consoles := resp.Consoles

			if _, ok := consoles[console]; ok {
				return nil
			} else {
				keys := make([]string, 0, len(consoles))
				for k := range consoles {
					keys = append(keys, k)
				}
				log.Printf("console %s not found, found: %v", console, keys)
			}
		}
	}
}

func FetchPorts() ([]domain.AugmentedPort, error) {
	body, err := FetchBytes("http://localhost:8081/api/v1/ports")
	if err != nil {
		return nil, err
	}
	var ap []domain.AugmentedPort
	json.Unmarshal(body, &ap)
	return ap, nil
}
