package services

import (
	"fmt"
	"log"

	"github.com/hashicorp/go-getter"
)

type RestoreService struct {
}

func NewRestoreService() *RestoreService {
	return &RestoreService{}
}

func (rc *RestoreService) Snapshot(dir string, destination string) error {

	// Define the source URL and destination directory
	source := "https://example.com/sample.txt" // Replace with a valid URL

	// Create a new client
	client := &getter.Client{
		Src:  source,                // Source URL
		Dst:  destination,           // Destination path
		Mode: getter.ClientModeFile, // Download as a file
	}

	// Download the file
	fmt.Println("Starting download...")
	err := client.Get()
	if err != nil {
		log.Fatalf("Error while downloading: %v", err)
	}

	fmt.Printf("File successfully downloaded to %s\n", destination)

	return nil
}

func (rc *RestoreService) RestoreSnapshot(dir string, source string) error {
	return nil
}
