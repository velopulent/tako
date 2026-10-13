package platform

import (
	"testing"
	"time"
)

func TestApplyRatesUsesThePreviousSample(t *testing.T) {
	now := time.Now()
	item := Process{CPUTime: 12, DiskRead: 4096, DiskWrite: 1024}
	applyRates(&item, ProcessResourceSample{Timestamp: now.Add(-2 * time.Second), CPUTime: 11, DiskRead: 0, DiskWrite: 1024}, now)
	if item.CPUPercent < 49 || item.CPUPercent > 51 || item.DiskReadRate != 2048 || item.DiskWriteRate != 0 {
		t.Fatalf("rates = %+v", item)
	}
	reset := Process{CPUTime: 1}
	applyRates(&reset, ProcessResourceSample{Timestamp: now.Add(-time.Second), CPUTime: 5}, now)
	if reset.CPUPercent != 0 {
		t.Fatalf("counter reset produced %v%%", reset.CPUPercent)
	}
}
