package metrics

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Sample struct {
	Timestamp      time.Time                  `json:"timestamp"`
	CPUPercent     float64                    `json:"cpuPercent"`
	MemoryUsed     uint64                     `json:"memoryUsed"`
	MemoryTotal    uint64                     `json:"memoryTotal"`
	Load1          float64                    `json:"load1"`
	Load5          float64                    `json:"load5"`
	Load15         float64                    `json:"load15"`
	CPUCorePercent []float64                  `json:"cpuCorePercent"`
	SwapUsed       uint64                     `json:"swapUsed"`
	SwapTotal      uint64                     `json:"swapTotal"`
	NetworkRX      uint64                     `json:"networkRx"`
	NetworkTX      uint64                     `json:"networkTx"`
	DiskRead       uint64                     `json:"diskRead"`
	DiskWrite      uint64                     `json:"diskWrite"`
	Disks          map[string]DiskSample      `json:"disks"`
	Interfaces     map[string]InterfaceSample `json:"interfaces"`
}

type InterfaceSample struct {
	RX uint64 `json:"rx"`
	TX uint64 `json:"tx"`
}

type DiskSample struct {
	Read  uint64 `json:"read"`
	Write uint64 `json:"write"`
}

type cpuCounters struct{ total, idle uint64 }

type Sampler struct {
	mu              sync.RWMutex
	samples         []Sample
	capacity        int
	previousCPU     cpuCounters
	previousCores   []cpuCounters
	subscribers     map[chan Sample]subscription
	wake            chan struct{}
	defaultInterval time.Duration
	retention       time.Duration
}

type subscription struct {
	interval time.Duration
	last     time.Time
}

func NewSampler(capacity int) *Sampler {
	return &Sampler{capacity: max(capacity, 2), subscribers: make(map[chan Sample]subscription), wake: make(chan struct{}, 1), defaultInterval: time.Minute, retention: 24 * time.Hour}
}

func (sampler *Sampler) Configure(defaultInterval, retention time.Duration) {
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	if defaultInterval >= time.Second {
		sampler.defaultInterval = defaultInterval
	}
	if retention > 0 {
		sampler.retention = retention
	}
}

func (sampler *Sampler) Run(ctx context.Context, interval time.Duration) {
	if interval >= time.Second {
		sampler.mu.Lock()
		sampler.defaultInterval = interval
		sampler.mu.Unlock()
	}
	sampler.collect()
	for {
		wait := sampler.fastestInterval()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-sampler.wake:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			sampler.collect()
		}
	}
}

func (sampler *Sampler) fastestInterval() time.Duration {
	sampler.mu.RLock()
	defer sampler.mu.RUnlock()
	result := sampler.defaultInterval
	for _, subscriber := range sampler.subscribers {
		if subscriber.interval < result {
			result = subscriber.interval
		}
	}
	return result
}

func (sampler *Sampler) History() []Sample {
	sampler.mu.RLock()
	defer sampler.mu.RUnlock()
	result := make([]Sample, len(sampler.samples))
	copy(result, sampler.samples)
	return result
}

func (sampler *Sampler) HistorySince(since time.Time, maximum int) []Sample {
	all := sampler.History()
	start := 0
	for start < len(all) && all[start].Timestamp.Before(since) {
		start++
	}
	all = all[start:]
	if maximum < 1 || len(all) <= maximum {
		return all
	}
	if maximum == 1 {
		return all[len(all)-1:]
	}
	step := float64(len(all)-1) / float64(maximum-1)
	result := make([]Sample, 0, maximum)
	for index := 0; index < maximum; index++ {
		result = append(result, all[int(float64(index)*step)])
	}
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
	sampler.mu.RLock()
	interval := sampler.defaultInterval
	sampler.mu.RUnlock()
	return sampler.SubscribeEvery(interval)
}

func (sampler *Sampler) SubscribeEvery(interval time.Duration) (<-chan Sample, func()) {
	interval = max(interval, time.Second)
	channel := make(chan Sample, 4)
	sampler.mu.Lock()
	sampler.subscribers[channel] = subscription{interval: interval}
	sampler.mu.Unlock()
	select {
	case sampler.wake <- struct{}{}:
	default:
	}
	return channel, func() {
		sampler.mu.Lock()
		if _, exists := sampler.subscribers[channel]; exists {
			delete(sampler.subscribers, channel)
			close(channel)
		}
		sampler.mu.Unlock()
		select {
		case sampler.wake <- struct{}{}:
		default:
		}
	}
}

func (sampler *Sampler) collect() {
	cpu, err := readCPU("/proc/stat")
	if err != nil {
		return
	}
	memoryUsed, memoryTotal, swapUsed, swapTotal, _ := readMemory("/proc/meminfo")
	load1, load5, load15, _ := readLoad("/proc/loadavg")
	rx, tx, interfaces, _ := readNetwork("/proc/net/dev")
	cumulativeDisks, _ := readDisks("/proc/diskstats", "/sys/block")
	cores, _ := readCPUCores("/proc/stat")

	sampler.mu.Lock()
	cpuPercent := 0.0
	if sampler.previousCPU.total > 0 && cpu.total > sampler.previousCPU.total {
		totalDelta := cpu.total - sampler.previousCPU.total
		idleDelta := cpu.idle - sampler.previousCPU.idle
		cpuPercent = 100 * float64(totalDelta-idleDelta) / float64(totalDelta)
	}
	sampler.previousCPU = cpu
	corePercents := make([]float64, len(cores))
	for index, counters := range cores {
		if index < len(sampler.previousCores) && counters.total > sampler.previousCores[index].total {
			total := counters.total - sampler.previousCores[index].total
			idle := counters.idle - sampler.previousCores[index].idle
			corePercents[index] = 100 * float64(total-idle) / float64(total)
		}
	}
	sampler.previousCores = cores
	diskRead, diskWrite := aggregateDisks(cumulativeDisks)
	now := time.Now().UTC()
	sample := Sample{Timestamp: now, CPUPercent: cpuPercent, CPUCorePercent: corePercents, MemoryUsed: memoryUsed, MemoryTotal: memoryTotal, SwapUsed: swapUsed, SwapTotal: swapTotal, Load1: load1, Load5: load5, Load15: load15, NetworkRX: rx, NetworkTX: tx, DiskRead: diskRead, DiskWrite: diskWrite, Disks: cumulativeDisks, Interfaces: interfaces}
	cutoff := now.Add(-sampler.retention)
	for len(sampler.samples) > 0 && sampler.samples[0].Timestamp.Before(cutoff) {
		sampler.samples = sampler.samples[1:]
	}
	historyInterval := sampler.retention / time.Duration(sampler.capacity-1)
	if historyInterval < time.Second {
		historyInterval = time.Second
	}
	if len(sampler.samples) > 0 && sampler.samples[len(sampler.samples)-1].Timestamp.Truncate(historyInterval) == now.Truncate(historyInterval) {
		sampler.samples[len(sampler.samples)-1] = sample
	} else if len(sampler.samples) == sampler.capacity {
		copy(sampler.samples, sampler.samples[1:])
		sampler.samples[len(sampler.samples)-1] = sample
	} else {
		sampler.samples = append(sampler.samples, sample)
	}
	for channel, subscriber := range sampler.subscribers {
		if !subscriber.last.IsZero() && now.Sub(subscriber.last) < subscriber.interval {
			continue
		}
		select {
		case channel <- sample:
			subscriber.last = now
			sampler.subscribers[channel] = subscriber
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

func readMemory(path string) (uint64, uint64, uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, 0, err
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
	swapTotal, swapFree := values["SwapTotal"], values["SwapFree"]
	return total - available, total, swapTotal - swapFree, swapTotal, scanner.Err()
}

func readLoad(path string) (float64, float64, float64, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
	}
	fields := strings.Fields(string(payload))
	if len(fields) == 0 {
		return 0, 0, 0, fmt.Errorf("invalid loadavg")
	}
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid loadavg")
	}
	one, err := strconv.ParseFloat(fields[0], 64)
	five, _ := strconv.ParseFloat(fields[1], 64)
	fifteen, _ := strconv.ParseFloat(fields[2], 64)
	return one, five, fifteen, err
}

func readNetwork(path string) (uint64, uint64, map[string]InterfaceSample, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, nil, err
	}
	defer file.Close()
	var rx, tx uint64
	interfaces := map[string]InterfaceSample{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		name := strings.TrimSpace(parts[0])
		if name == "lo" {
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
		interfaces[name] = InterfaceSample{RX: received, TX: transmitted}
	}
	return rx, tx, interfaces, scanner.Err()
}

func readCPUCores(path string) ([]cpuCounters, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := []cpuCounters{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") || fields[0] == "cpu" {
			continue
		}
		var total uint64
		values := make([]uint64, len(fields)-1)
		for i := range values {
			values[i], _ = strconv.ParseUint(fields[i+1], 10, 64)
			total += values[i]
		}
		result = append(result, cpuCounters{total: total, idle: values[3] + values[4]})
	}
	return result, scanner.Err()
}

// readDisks reports cumulative byte counters per whole physical disk, mirroring
// the network interface counters. Only devices enumerated under /sys/block are
// counted, so partition rows in /proc/diskstats never double count against
// their parent disk; loop, ram, zram, cdrom, floppy, network-block,
// device-mapper, and RAID layers are excluded (RAID member disks too) so each
// sector is attributed once.
func readDisks(statsPath, blockDir string) (map[string]DiskSample, error) {
	disks, err := wholeDisks(blockDir)
	if err != nil {
		return nil, err
	}
	counters, err := parseDiskStats(statsPath)
	if err != nil {
		return nil, err
	}
	result := make(map[string]DiskSample, len(disks))
	for name := range disks {
		counter, ok := counters[name]
		if !ok {
			continue
		}
		result[name] = DiskSample{Read: counter.read * 512, Write: counter.write * 512}
	}
	return result, nil
}

func aggregateDisks(disks map[string]DiskSample) (uint64, uint64) {
	var read, written uint64
	for _, disk := range disks {
		read += disk.Read
		written += disk.Write
	}
	return read, written
}

func wholeDisks(blockDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(blockDir)
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{}
	disks := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if isExcludedDevice(name) {
			if members, err := os.ReadDir(filepath.Join(blockDir, name, "slaves")); err == nil {
				for _, member := range members {
					excluded[member.Name()] = true
				}
			}
			continue
		}
		disks[name] = true
	}
	for name := range excluded {
		delete(disks, name)
	}
	return disks, nil
}

func isExcludedDevice(name string) bool {
	for _, prefix := range []string{"loop", "ram", "zram", "dm-", "md", "sr", "fd", "nbd"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func parseDiskStats(path string) (map[string]diskCounters, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := map[string]diskCounters{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		sectorsRead, _ := strconv.ParseUint(fields[5], 10, 64)
		sectorsWritten, _ := strconv.ParseUint(fields[9], 10, 64)
		result[fields[2]] = diskCounters{read: sectorsRead, write: sectorsWritten}
	}
	return result, scanner.Err()
}

type diskCounters struct{ read, write uint64 }
