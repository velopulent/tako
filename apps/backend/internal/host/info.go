package host

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Info struct {
	Hostname        string    `json:"hostname"`
	OperatingSystem string    `json:"operatingSystem"`
	Kernel          string    `json:"kernel"`
	Architecture    string    `json:"architecture"`
	UptimeSeconds   uint64    `json:"uptimeSeconds"`
	BootedAt        time.Time `json:"bootedAt"`
}

func Read() Info {
	hostname, _ := os.Hostname()
	uptime := readUptime()
	return Info{
		Hostname:        hostname,
		OperatingSystem: readOSRelease(),
		Kernel:          readKernel(),
		Architecture:    runtime.GOARCH,
		UptimeSeconds:   uptime,
		BootedAt:        time.Now().Add(-time.Duration(uptime) * time.Second).UTC(),
	}
}

func readUptime() uint64 {
	payload, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	value, _ := strconv.ParseFloat(strings.Fields(string(payload))[0], 64)
	return uint64(value)
}

func readOSRelease() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(scanner.Text(), "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

func readKernel() string {
	var info syscall.Utsname
	if syscall.Uname(&info) != nil {
		return "unknown"
	}
	bytes := make([]byte, 0, len(info.Release))
	for _, value := range info.Release {
		if value == 0 {
			break
		}
		bytes = append(bytes, byte(value))
	}
	return string(bytes)
}
