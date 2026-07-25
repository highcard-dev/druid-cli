package domain

import (
	"math"
	"testing"
)

func TestSnapshotProgressStoresTenths(t *testing.T) {
	progress := NewSnapshotProgress()
	if !progress.StorePercentage(54.71) {
		t.Fatal("valid percentage was rejected")
	}
	if got := progress.Percentage(); got != 54.7 {
		t.Fatalf("percentage = %v; want 54.7", got)
	}
}

func TestSnapshotProgressClampsAndRejectsInvalidValues(t *testing.T) {
	progress := NewSnapshotProgress()
	progress.StorePercentage(150)
	if got := progress.Percentage(); got != 100 {
		t.Fatalf("percentage = %v; want 100", got)
	}
	progress.StorePercentage(-10)
	if got := progress.Percentage(); got != 0 {
		t.Fatalf("percentage = %v; want 0", got)
	}
	if progress.StorePercentage(math.NaN()) {
		t.Fatal("NaN was accepted")
	}
}
