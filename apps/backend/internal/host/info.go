package host

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Hardware struct {
	Available       bool   `json:"available"`
	CPUModel        string `json:"cpuModel,omitempty"`
	CPUCores        int    `json:"cpuCores,omitempty"`
	MemoryTotal     uint64 `json:"memoryTotal,omitempty"`
	MemoryAvailable uint64 `json:"memoryAvailable,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type ShutdownStatus struct {
	Available bool   `json:"available"`
	Clean     bool   `json:"clean"`
	Reason    string `json:"reason,omitempty"`
}

type RestartStatus struct {
	Available bool   `json:"available"`
	Required  bool   `json:"required"`
	Source    string `json:"source,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type Info struct {
	Hostname        string         `json:"hostname"`
	OperatingSystem string         `json:"operatingSystem"`
	Kernel          string         `json:"kernel"`
	Architecture    string         `json:"architecture"`
	UptimeSeconds   uint64         `json:"uptimeSeconds"`
	BootedAt        time.Time      `json:"bootedAt"`
	BootID          string         `json:"bootId,omitempty"`
	Hardware        Hardware       `json:"hardware"`
	Shutdown        ShutdownStatus `json:"shutdown"`
	Restart         RestartStatus  `json:"restart"`
}

func Read() Info {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return ReadContext(ctx)
}

func ReadContext(ctx context.Context) Info {
	hostname, _ := os.Hostname()
	uptime := readUptime()
	return Info{
		Hostname:        hostname,
		OperatingSystem: readOSRelease(),
		Kernel:          readKernel(),
		Architecture:    runtime.GOARCH,
		UptimeSeconds:   uptime,
		BootedAt:        time.Now().Add(-time.Duration(uptime) * time.Second).UTC(),
		BootID:          readBootID(),
		Hardware:        readHardware(),
		Shutdown:        readShutdown(ctx),
		Restart:         readRestart(ctx),
	}
}

func readBootID() string {
	payload, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(payload))
}

func readHardware() Hardware {
	result := Hardware{CPUCores: runtime.NumCPU()}
	file, err := os.Open("/proc/cpuinfo")
	if err == nil {
		scanner := bufio.NewScanner(io.LimitReader(file, 1<<20))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") {
				if fields := strings.SplitN(line, ":", 2); len(fields) == 2 {
					result.CPUModel = strings.TrimSpace(fields[1])
					break
				}
			}
		}
		_ = file.Close()
	}
	if memory, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(io.LimitReader(memory, 1<<20))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 {
				continue
			}
			value, parseErr := strconv.ParseUint(fields[1], 10, 64)
			if parseErr != nil {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				result.MemoryTotal = value * 1024
			case "MemAvailable:":
				result.MemoryAvailable = value * 1024
			}
		}
		_ = memory.Close()
	}
	result.Available = result.CPUCores > 0 && result.MemoryTotal > 0
	if !result.Available {
		result.Reason = "Hardware details are unavailable from procfs."
	}
	return result
}

func readShutdown(ctx context.Context) ShutdownStatus {
	output, err := boundedCommand(ctx, 750*time.Millisecond, "/usr/bin/journalctl", "--boot=-1", "--lines=20", "--no-pager", "--output=cat")
	if err != nil {
		return ShutdownStatus{Reason: "Previous shutdown history is unavailable without readable journald history."}
	}
	lower := strings.ToLower(output)
	clean := strings.Contains(lower, "systemd-shutdown") || strings.Contains(lower, "reached target shutdown") || strings.Contains(lower, "shutting down")
	if !clean {
		return ShutdownStatus{Reason: "Previous boot ended without a recognizable clean-shutdown marker."}
	}
	return ShutdownStatus{Available: true, Clean: true}
}

func readRestart(ctx context.Context) RestartStatus {
	if _, err := os.Stat("/run/reboot-required"); err == nil {
		reason := "The operating system reports that a restart is required."
		if packages, readErr := os.ReadFile("/run/reboot-required.pkgs"); readErr == nil {
			values := strings.Fields(string(packages))
			if len(values) > 0 {
				reason = fmt.Sprintf("Restart required after updates to %s.", strings.Join(values[:min(len(values), 8)], ", "))
			}
		}
		return RestartStatus{Available: true, Required: true, Source: "reboot-required", Reason: reason}
	}
	for _, command := range []string{"/usr/bin/needs-restarting", "/usr/sbin/needs-restarting"} {
		if _, err := os.Stat(command); err != nil {
			continue
		}
		output, err := boundedCommand(ctx, time.Second, command, "-r")
		if err == nil {
			return RestartStatus{Available: true, Source: "needs-restarting", Reason: strings.TrimSpace(output)}
		}
		if strings.TrimSpace(output) != "" || errorsIsExitStatus(err, 1) {
			return RestartStatus{Available: true, Required: true, Source: "needs-restarting", Reason: "The package manager reports that a restart is required."}
		}
	}
	return RestartStatus{Reason: "No supported restart-status provider is available."}
}

func boundedCommand(parent context.Context, timeout time.Duration, name string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, arguments...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := command.Start(); err != nil {
		return "", err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, 64<<10+1))
	if len(output) > 64<<10 {
		_ = command.Process.Kill()
		_ = command.Wait()
		return "", errors.New("command output exceeded limit")
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if readErr != nil {
		return "", readErr
	}
	if waitErr != nil {
		return string(bytes.TrimSpace(output)), waitErr
	}
	return string(output), nil
}

func errorsIsExitStatus(err error, status int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == status
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func readUptime() uint64 {
	payload, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(payload))
	if len(fields) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[0], 64)
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
