package metrics

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Sample struct {
	Timestamp   time.Time `json:"timestamp"`
	CPUPercent  float64   `json:"cpuPercent"`
	MemoryUsed  uint64    `json:"memoryUsed"`
	MemoryTotal uint64    `json:"memoryTotal"`
	Load1       float64   `json:"load1"`
	NetworkRX   uint64    `json:"networkRx"`
	NetworkTX   uint64    `json:"networkTx"`
}

type cpuCounters struct{ total, idle uint64 }

type Sampler struct {
	mu          sync.RWMutex
	samples     []Sample
	capacity    int
	previousCPU cpuCounters
	subscribers map[chan Sample]struct{}
}

func NewSampler(capacity int) *Sampler {
	return &Sampler{capacity: capacity, subscribers: make(map[chan Sample]struct{})}
}

func (sampler *Sampler) Run(ctx context.Context, interval time.Duration) {
	sampler.collect()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sampler.collect()
		}
	}
}

func (sampler *Sampler) History() []Sample {
	sampler.mu.RLock()
	defer sampler.mu.RUnlock()
	result := make([]Sample, len(sampler.samples))
	copy(result, sampler.samples)
	return result
}

func (sampler *Sampler) Current() (Sample, bool) {
	sampler.mu.RLock()
	defer sampler.mu.RUnlock()
	if len(sampler.samples) == 0 {
		return Sample{}, false
	}
	return sampler.samples[len(sampler.samples)-1], true
}

func (sampler *Sampler) Subscribe() (<-chan Sample, func()) {
	channel := make(chan Sample, 4)
	sampler.mu.Lock()
	sampler.subscribers[channel] = struct{}{}
	sampler.mu.Unlock()
	return channel, func() {
		sampler.mu.Lock()
		delete(sampler.subscribers, channel)
		close(channel)
		sampler.mu.Unlock()
	}
}

func (sampler *Sampler) collect() {
	cpu, err := readCPU("/proc/stat")
	if err != nil {
		return
	}
	memoryUsed, memoryTotal, _ := readMemory("/proc/meminfo")
	load, _ := readLoad("/proc/loadavg")
	rx, tx, _ := readNetwork("/proc/net/dev")

	sampler.mu.Lock()
	cpuPercent := 0.0
	if sampler.previousCPU.total > 0 && cpu.total > sampler.previousCPU.total {
		totalDelta := cpu.total - sampler.previousCPU.total
		idleDelta := cpu.idle - sampler.previousCPU.idle
		cpuPercent = 100 * float64(totalDelta-idleDelta) / float64(totalDelta)
	}
	sampler.previousCPU = cpu
	sample := Sample{Timestamp: time.Now().UTC(), CPUPercent: cpuPercent, MemoryUsed: memoryUsed, MemoryTotal: memoryTotal, Load1: load, NetworkRX: rx, NetworkTX: tx}
	if len(sampler.samples) == sampler.capacity {
		copy(sampler.samples, sampler.samples[1:])
		sampler.samples[len(sampler.samples)-1] = sample
	} else {
		sampler.samples = append(sampler.samples, sample)
	}
	for subscriber := range sampler.subscribers {
		select {
		case subscriber <- sample:
		default:
		}
	}
	sampler.mu.Unlock()
}

func readCPU(path string) (cpuCounters, error) {
	file, err := os.Open(path)
	if err != nil {
		return cpuCounters{}, err
	}
	defer file.Close()
	var label string
	values := make([]uint64, 10)
	_, err = fmt.Fscan(file, &label, &values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &values[7], &values[8], &values[9])
	if err != nil || label != "cpu" {
		return cpuCounters{}, fmt.Errorf("invalid proc stat")
	}
	total := uint64(0)
	for _, value := range values {
		total += value
	}
	return cpuCounters{total: total, idle: values[3] + values[4]}, nil
}

func readMemory(path string) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		values[strings.TrimSuffix(fields[0], ":")] = value * 1024
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	return total - available, total, scanner.Err()
}

func readLoad(path string) (float64, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(payload))
	if len(fields) == 0 {
		return 0, fmt.Errorf("invalid loadavg")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func readNetwork(path string) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	var rx, tx uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if strings.TrimSpace(parts[0]) == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		received, _ := strconv.ParseUint(fields[0], 10, 64)
		transmitted, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += received
		tx += transmitted
	}
	return rx, tx, scanner.Err()
}
