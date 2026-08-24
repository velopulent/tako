package platform

import (
	"context"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/packagekit"
)

// UpdateLive is a point-in-time observation of the running package-update
// transaction. The gateway reads it straight from PackageKit so it can render
// progress for updates started by sessiond, pkcon, or an external dnf client.
type UpdateLive struct {
	Active           bool   `json:"active"`
	Source           string `json:"source,omitempty"`
	Percentage       int    `json:"percentage"`
	AllowCancel      bool   `json:"allowCancel"`
	Status           string `json:"status,omitempty"`
	CurrentPackage   string `json:"currentPackage,omitempty"`
	RemainingSeconds int64  `json:"remainingSeconds,omitempty"`
	TransactionPath  string `json:"transactionPath,omitempty"`
}

// unknownProgress matches PackageKit's Percentage=101 sentinel.
const unknownProgress = -1

// UpdateLiveStatus snapshots the running update transaction, or reports an
// inactive snapshot when nothing is in flight.
func UpdateLiveStatus(ctx context.Context) UpdateLive {
	client, err := packagekit.New()
	if err != nil {
		return InactiveUpdateLive()
	}
	defer client.Close()
	if !client.Detect(ctx) {
		return InactiveUpdateLive()
	}
	return UpdateLiveFromSnapshot(client.UpdateSnapshot(ctx))
}

// InactiveUpdateLive reports no running update.
func InactiveUpdateLive() UpdateLive {
	return UpdateLive{Active: false, Percentage: unknownProgress}
}

// UpdateLiveFromSnapshot converts a raw PackageKit snapshot into the API shape;
// a nil snapshot becomes an inactive report.
func UpdateLiveFromSnapshot(snapshot *packagekit.LiveUpdateSnapshot) UpdateLive {
	if snapshot == nil {
		return InactiveUpdateLive()
	}
	live := UpdateLive{
		Active:          true,
		Source:          "packagekit",
		Status:          snapshot.StatusMessage,
		CurrentPackage:  FormatPackageID(snapshot.LastPackage),
		AllowCancel:     snapshot.AllowCancel,
		TransactionPath: snapshot.TransactionPath,
	}
	if snapshot.Percentage <= 100 {
		live.Percentage = int(snapshot.Percentage)
	} else {
		live.Percentage = unknownProgress
	}
	if snapshot.RemainingTime > 0 {
		live.RemainingSeconds = snapshot.RemainingTime
	}
	return live
}

// FormatPackageID renders a PackageKit package id ("name;version;arch;repo")
// as "name version (arch)".
func FormatPackageID(packageID string) string {
	fields := strings.SplitN(packageID, ";", 4)
	if len(fields) < 3 || fields[0] == "" {
		return ""
	}
	name := fields[0] + " " + fields[1]
	if fields[2] != "" {
		name += " (" + fields[2] + ")"
	}
	return name
}

// UpdateHistoryEntry is one past update transaction: wall-clock time plus the
// packages it touched (name -> version).
type UpdateHistoryEntry struct {
	Time     int64             `json:"time"`
	Packages map[string]string `json:"packages"`
}

// MaxUpdateHistory bounds the returned history window.
const MaxUpdateHistory = 20

// UpdateHistory returns recent package-update transactions, newest first.
func UpdateHistory(ctx context.Context) ([]UpdateHistoryEntry, error) {
	client, err := packagekit.New()
	if err != nil {
		return nil, ErrUpdateUnavailable
	}
	defer client.Close()
	if !client.Detect(ctx) {
		return nil, ErrUpdateUnavailable
	}
	historyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	entries, err := client.GetOldTransactions(historyCtx)
	if err != nil {
		return nil, err
	}
	result := make([]UpdateHistoryEntry, 0, len(entries))
	for _, entry := range entries {
		packages := entry.Packages
		if packages == nil {
			packages = map[string]string{}
		}
		result = append(result, UpdateHistoryEntry{Time: entry.Time, Packages: packages})
	}
	return result, nil
}

// CancelRunningUpdate cancels the currently active package-update transaction
// regardless of who started it. It returns false when no transaction was
// running.
func CancelRunningUpdate(ctx context.Context) (bool, error) {
	client, err := packagekit.New()
	if err != nil {
		return false, ErrUpdateUnavailable
	}
	defer client.Close()
	if !client.Detect(ctx) {
		return false, ErrUpdateUnavailable
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return client.CancelActiveUpdate(cancelCtx)
}
