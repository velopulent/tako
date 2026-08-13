package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrInvalidSupportReport = errors.New("invalid support report request")

type SupportReportOperation struct {
	Confirmation string `json:"confirmation"`
}

type SupportReport struct {
	Path      string    `json:"path"`
	Bytes     int64     `json:"bytes"`
	CreatedAt time.Time `json:"createdAt"`
	Warning   string    `json:"warning"`
}

func ValidateSupportReportOperation(operation SupportReportOperation) error {
	if operation.Confirmation != "CONFIRM SUPPORT REPORT" {
		return ErrInvalidSupportReport
	}
	return nil
}

func CollectSupportReport(ctx context.Context, operation SupportReportOperation) (SupportReport, error) {
	if err := ValidateSupportReportOperation(operation); err != nil {
		return SupportReport{}, err
	}
	directory := "/var/lib/tako/support"
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return SupportReport{}, err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return SupportReport{}, err
	}
	output, err := networkCommand(ctx, "sos", "report", "--batch", "--no-progress", "--tmp-dir", directory)
	if err != nil {
		return SupportReport{}, err
	}
	var total int64
	var latest string
	entries := 0
	walkErr := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		entries++
		if entries > 4096 || info.Size() > 256<<20 || total+info.Size() > 256<<20 {
			return errors.New("support report exceeds bounded size")
		}
		total += info.Size()
		latest = path
		return nil
	})
	if walkErr != nil {
		return SupportReport{}, walkErr
	}
	if latest == "" {
		latest = strings.TrimSpace(string(output))
	}
	return SupportReport{Path: filepath.Clean(latest), Bytes: total, CreatedAt: time.Now().UTC(), Warning: fmt.Sprintf("Support reports can contain sensitive host metadata; review and redact before sharing. Bounded output: %d bytes.", total)}, nil
}
