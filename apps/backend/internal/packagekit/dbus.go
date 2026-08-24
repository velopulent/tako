package packagekit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	dbusDest           = "org.freedesktop.PackageKit"
	dbusPath           = "/org/freedesktop/PackageKit"
	dbusInterface      = "org.freedesktop.PackageKit"
	transactionIface   = "org.freedesktop.PackageKit.Transaction"
	transactionsPath   = "/org/freedesktop/PackageKit/transactions"
	RoleRefreshCache   = 13
	RoleGetUpdates     = 14
	RoleUpdatePackages = 22
)

// Transaction status enum values from lib/pk-enum.h (verified against the
// PackageKit source).
const (
	StatusWait           = 1
	StatusWaitingForLock = 30
	StatusFinished       = 18
	StatusCanceled       = 19
)

var (
	ErrPackageKitNotAvailable = errors.New("PackageKit not available")
	ErrTransactionFailed      = errors.New("PackageKit transaction failed")
)

// Update holds one PackageKit UpdateDetail result.
type Update struct {
	ID          string
	Name        string
	Version     string
	Arch        string
	Summary     string
	Severity    string // security, bugfix, enhancement
	Description string
	Markdown    bool
	BugURLs     []string
	CVEURLs     []string
	VendorURLs  []string
}

// Client wraps a D-Bus connection to PackageKit.
type Client struct {
	conn *dbus.Conn
}

// New creates a new PackageKit client with a system bus connection
func New() (*Client, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

// NewWithConn for testing
func NewWithConn(conn *dbus.Conn) *Client {
	return &Client{conn: conn}
}

func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// Detect checks if PackageKit is available via VersionMajor property.
func (c *Client) Detect(ctx context.Context) bool {
	if c.conn == nil {
		conn, err := dbus.ConnectSystemBus()
		if err != nil {
			return false
		}
		defer conn.Close()
		c = &Client{conn: conn}
	}
	obj := c.conn.Object(dbusDest, dbusPath)
	var version uint32
	err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, dbusInterface, "VersionMajor").Store(&version)
	if err == nil {
		return true
	}
	// also try ListNames fallback like old code
	return packageKitAvailableFallback(ctx, c.conn)
}

func packageKitAvailableFallback(ctx context.Context, conn *dbus.Conn) bool {
	var names []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names); err == nil {
		for _, n := range names {
			if n == "org.freedesktop.PackageKit" {
				return true
			}
		}
	}
	var activatable []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err == nil {
		for _, n := range activatable {
			if n == "org.freedesktop.PackageKit" {
				return true
			}
		}
	}
	return false
}

// GetTimeSinceAction returns seconds elapsed since the given role last ran.
func (c *Client) GetTimeSinceAction(ctx context.Context, role uint32) (int64, error) {
	obj := c.conn.Object(dbusDest, dbusPath)
	var seconds int64
	// PackageKit GetTimeSinceAction takes role enum
	err := obj.CallWithContext(ctx, dbusInterface+".GetTimeSinceAction", 0, role).Store(&seconds)
	if err != nil {
		return 0, err
	}
	return seconds, nil
}

// GetUpdates fetches updates with details.
func (c *Client) GetUpdates(ctx context.Context) ([]Update, error) {
	updatesMap, err := c.getUpdatesRaw(ctx)
	if err != nil {
		return nil, err
	}
	if len(updatesMap) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(updatesMap))
	for id := range updatesMap {
		ids = append(ids, id)
	}
	// Batch GetUpdateDetail (500 per batch, retry singles on failure).
	if err := c.loadUpdateDetailsBatched(ctx, ids, updatesMap); err != nil {
		// Continue with partial details.
		fmt.Printf("warning: loadUpdateDetails failed: %v\n", err)
	}
	var out []Update
	for _, u := range updatesMap {
		out = append(out, *u)
	}
	return out, nil
}

func (c *Client) getUpdatesRaw(ctx context.Context) (map[string]*Update, error) {
	transactionPath, err := c.createTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer c.removeTransactionSignal(transactionPath)

	updates := make(map[string]*Update)
	// Set up signal handling for Package
	sigChan := make(chan *dbus.Signal, 32)
	c.conn.Signal(sigChan)
	defer c.conn.RemoveSignal(sigChan)
	// Add match for this transaction
	_ = c.conn.AddMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))
	defer c.conn.RemoveMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))

	// Channel to collect result
	errChan := make(chan error, 1)
	finished := make(chan uint32, 1)

	go func() {
		for sig := range sigChan {
			if sig.Path != transactionPath {
				continue
			}
			switch sig.Name {
			case transactionIface + ".Package":
				if len(sig.Body) < 3 {
					continue
				}
				info, _ := sig.Body[0].(uint32)
				packageID, _ := sig.Body[1].(string)
				summary, _ := sig.Body[2].(string)
				severity := mapInfoToSeverity(info)
				fields := strings.Split(packageID, ";")
				if len(fields) < 3 {
					continue
				}
				name := fields[0]
				version := fields[1]
				arch := fields[2]
				// HACK: dnf backend yields wrong severity with PK <1.2.4;
				// handled by the range check in mapInfoToSeverity.
				u := &Update{
					ID:       packageID,
					Name:     name,
					Version:  version,
					Arch:     arch,
					Summary:  summary,
					Severity: severity,
				}
				updates[packageID] = u
			case transactionIface + ".ErrorCode":
				// collect for later, but don't fail immediately
				if len(sig.Body) >= 2 {
					// code, details := sig.Body[0], sig.Body[1]
				}
			case transactionIface + ".Finished":
				if len(sig.Body) >= 1 {
					if exit, ok := sig.Body[0].(uint32); ok {
						finished <- exit
						return
					}
				}
				finished <- 0
				return
			}
		}
	}()

	// Call GetUpdates
	obj := c.conn.Object(dbusDest, transactionPath)
	call := obj.CallWithContext(ctx, transactionIface+".GetUpdates", 0, uint32(0))
	if call.Err != nil {
		return nil, call.Err
	}

	select {
	case exit := <-finished:
		if exit != 1 { // EXIT_SUCCESS =1 per packagekit.js Enum
			return nil, fmt.Errorf("%w: exit %d", ErrTransactionFailed, exit)
		}
		return updates, nil
	case <-ctx.Done():
		_ = c.cancelTransaction(transactionPath)
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		_ = c.cancelTransaction(transactionPath)
		return nil, errors.New("GetUpdates timeout")
	case err := <-errChan:
		return nil, err
	}
}

func (c *Client) loadUpdateDetailsBatched(ctx context.Context, ids []string, updates map[string]*Update) error {
	const initialBatch = 500
	batches := splitIntoBatches(ids, initialBatch)
	for _, batch := range batches {
		if err := c.loadUpdateDetailsBatch(ctx, batch, updates); err != nil {
			// Retry with batch size 1.
			if len(batch) > 1 {
				for _, single := range batch {
					_ = c.loadUpdateDetailsBatch(ctx, []string{single}, updates)
				}
			} else {
				return err
			}
		}
	}
	return nil
}

func (c *Client) loadUpdateDetailsBatch(ctx context.Context, batch []string, updates map[string]*Update) error {
	transactionPath, err := c.createTransaction(ctx)
	if err != nil {
		return err
	}
	defer c.removeTransactionSignal(transactionPath)

	sigChan := make(chan *dbus.Signal, 32)
	c.conn.Signal(sigChan)
	defer c.conn.RemoveSignal(sigChan)
	_ = c.conn.AddMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))
	defer c.conn.RemoveMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))

	finished := make(chan uint32, 1)
	go func() {
		for sig := range sigChan {
			if sig.Path != transactionPath {
				continue
			}
			switch sig.Name {
			case transactionIface + ".UpdateDetail":
				if len(sig.Body) < 9 {
					continue
				}
				packageID, _ := sig.Body[0].(string)
				// updates, obsoletes skipped
				vendorURLs, _ := sig.Body[3].([]string)
				bugURLs, _ := sig.Body[4].([]string)
				cveURLs, _ := sig.Body[5].([]string)
				// restart ignored
				updateText, _ := sig.Body[7].(string)
				changelog, _ := sig.Body[8].(string)
				u, ok := updates[packageID]
				if !ok {
					continue
				}
				u.VendorURLs = vendorURLs
				u.Description = removeHeading(updateText)
				if u.Description == "" {
					u.Description = changelog
				} else {
					u.Markdown = updateText != ""
				}
				u.BugURLs = deduplicate(bugURLs)
				// fallback CVE parse if empty
				if len(cveURLs) == 0 {
					cveURLs = parseCVEs(u.Description)
				} else {
					// Normalize and deduplicate the cve.org URLs.
					cveURLs = deduplicate(cveURLs)
				}
				u.CVEURLs = deduplicate(cveURLs)
				if len(u.CVEURLs) > 0 {
					u.Severity = "security"
				}
				u.VendorURLs = deduplicate(vendorURLs)
			case transactionIface + ".Finished":
				if len(sig.Body) >= 1 {
					if exit, ok := sig.Body[0].(uint32); ok {
						finished <- exit
						return
					}
				}
				finished <- 0
				return
			}
		}
	}()

	obj := c.conn.Object(dbusDest, transactionPath)
	call := obj.CallWithContext(ctx, transactionIface+".GetUpdateDetail", 0, batch)
	if call.Err != nil {
		return call.Err
	}

	select {
	case exit := <-finished:
		if exit != 1 {
			return fmt.Errorf("%w: GetUpdateDetail exit %d", ErrTransactionFailed, exit)
		}
		return nil
	case <-ctx.Done():
		_ = c.cancelTransaction(transactionPath)
		return ctx.Err()
	case <-time.After(15 * time.Second):
		_ = c.cancelTransaction(transactionPath)
		return errors.New("GetUpdateDetail timeout")
	}
}

func (c *Client) createTransaction(ctx context.Context) (dbus.ObjectPath, error) {
	obj := c.conn.Object(dbusDest, dbusPath)
	var path dbus.ObjectPath
	err := obj.CallWithContext(ctx, dbusInterface+".CreateTransaction", 0).Store(&path)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (c *Client) cancelTransaction(path dbus.ObjectPath) error {
	obj := c.conn.Object(dbusDest, path)
	return obj.Call("org.freedesktop.PackageKit.Transaction.Cancel", 0).Err
}

func (c *Client) removeTransactionSignal(path dbus.ObjectPath) {
	// placeholder for cleanup, matches removed via defer
}

// RefreshCache refreshes the PackageKit cache (force controls whether the
// cache is refreshed even when considered fresh).
func (c *Client) RefreshCache(ctx context.Context, force bool) error {
	transactionPath, err := c.createTransaction(ctx)
	if err != nil {
		return err
	}
	defer c.removeTransactionSignal(transactionPath)

	sigChan := make(chan *dbus.Signal, 16)
	c.conn.Signal(sigChan)
	defer c.conn.RemoveSignal(sigChan)
	_ = c.conn.AddMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))
	defer c.conn.RemoveMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))

	finished := make(chan uint32, 1)
	errorMsg := ""
	go func() {
		for sig := range sigChan {
			if sig.Path != transactionPath {
				continue
			}
			switch sig.Name {
			case transactionIface + ".ErrorCode":
				if len(sig.Body) >= 2 {
					if details, ok := sig.Body[1].(string); ok {
						errorMsg = details
					}
				}
			case transactionIface + ".Finished":
				if len(sig.Body) >= 1 {
					if exit, ok := sig.Body[0].(uint32); ok {
						finished <- exit
						return
					}
				}
				finished <- 0
				return
			}
		}
	}()

	obj := c.conn.Object(dbusDest, transactionPath)
	call := obj.CallWithContext(ctx, transactionIface+".RefreshCache", 0, force)
	if call.Err != nil {
		return call.Err
	}

	select {
	case exit := <-finished:
		if exit != 1 {
			if errorMsg != "" {
				return fmt.Errorf("%w: %s", ErrTransactionFailed, errorMsg)
			}
			return fmt.Errorf("%w: exit %d", ErrTransactionFailed, exit)
		}
		return nil
	case <-ctx.Done():
		_ = c.cancelTransaction(transactionPath)
		return ctx.Err()
	case <-time.After(5 * time.Minute):
		_ = c.cancelTransaction(transactionPath)
		return errors.New("RefreshCache timeout")
	}
}

// UpdatePackages runs an update transaction. Live progress is not streamed
// through this call: transactions are bus-visible objects, so the gateway
// observes Percentage/Status/LastPackage via UpdateSnapshot instead.
func (c *Client) UpdatePackages(ctx context.Context, packageIDs []string) error {
	transactionPath, err := c.createTransaction(ctx)
	if err != nil {
		return err
	}
	defer c.removeTransactionSignal(transactionPath)

	sigChan := make(chan *dbus.Signal, 32)
	c.conn.Signal(sigChan)
	defer c.conn.RemoveSignal(sigChan)
	_ = c.conn.AddMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))
	defer c.conn.RemoveMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))

	finished := make(chan uint32, 1)
	var errorDetail string
	go func() {
		for sig := range sigChan {
			if sig.Path != transactionPath {
				continue
			}
			switch sig.Name {
			case transactionIface + ".ErrorCode":
				if len(sig.Body) >= 2 {
					if d, ok := sig.Body[1].(string); ok {
						errorDetail = d
					}
				}
			case transactionIface + ".Finished":
				if len(sig.Body) >= 1 {
					if exit, ok := sig.Body[0].(uint32); ok {
						finished <- exit
						return
					}
				}
				finished <- 0
				return
			}
		}
	}()

	obj := c.conn.Object(dbusDest, transactionPath)
	call := obj.CallWithContext(ctx, transactionIface+".UpdatePackages", 0, uint32(0), packageIDs)
	if call.Err != nil {
		return call.Err
	}

	select {
	case exit := <-finished:
		if exit == 3 { // EXIT_CANCELLED
			return context.Canceled
		}
		if exit != 1 {
			if errorDetail != "" {
				return fmt.Errorf("%w: %s", ErrTransactionFailed, errorDetail)
			}
			return fmt.Errorf("%w: exit %d", ErrTransactionFailed, exit)
		}
		return nil
	case <-ctx.Done():
		_ = c.cancelTransaction(transactionPath)
		return ctx.Err()
	}
}

// GetTransactionList returns the current PackageKit transaction paths.
func (c *Client) GetTransactionList(ctx context.Context) ([]dbus.ObjectPath, error) {
	obj := c.conn.Object(dbusDest, dbusPath)
	var paths []dbus.ObjectPath
	err := obj.CallWithContext(ctx, dbusInterface+".GetTransactionList", 0).Store(&paths)
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// transactionUintProperty reads a uint32 property from a transaction object.
func (c *Client) transactionUintProperty(ctx context.Context, path dbus.ObjectPath, name string) (uint32, bool) {
	obj := c.conn.Object(dbusDest, path)
	var value uint32
	if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, transactionIface, name).Store(&value); err != nil {
		return 0, false
	}
	return value, true
}

func (c *Client) transactionStringProperty(ctx context.Context, path dbus.ObjectPath, name string) (string, bool) {
	obj := c.conn.Object(dbusDest, path)
	var value string
	if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, transactionIface, name).Store(&value); err != nil {
		return "", false
	}
	return value, true
}

// TransactionBusy reports whether the given transaction is an in-flight cache
// refresh or package update. Only WAIT / WAITING_FOR_LOCK style
// live transactions are blocking; FINISHED (18) and CANCEL (19) transactions can
// linger in the list briefly without holding any lock.
func (c *Client) TransactionBusy(ctx context.Context, path dbus.ObjectPath) (bool, string) {
	role, ok := c.transactionUintProperty(ctx, path, "Role")
	if !ok {
		// A vanished transaction cannot hold a lock; an unreadable one might.
		if _, statOk := c.transactionUintProperty(ctx, path, "Status"); !statOk {
			return false, ""
		}
		return true, "PackageKit transaction in progress"
	}
	if role != RoleRefreshCache && role != RoleUpdatePackages {
		return false, ""
	}
	status, ok := c.transactionUintProperty(ctx, path, "Status")
	if !ok {
		return true, "PackageKit transaction in progress"
	}
	if status == StatusFinished || status == StatusCanceled {
		return false, ""
	}
	if role == RoleRefreshCache {
		return true, "Refreshing package metadata"
	}
	return true, "A package update is in progress"
}

// LiveUpdateSnapshot describes the currently running package-update
// transaction. It is read directly from D-Bus properties so the gateway can
// observe updates started by sessiond, pkcon, or any other client.
type LiveUpdateSnapshot struct {
	TransactionPath string
	Percentage      uint32
	AllowCancel     bool
	Status          uint32
	StatusMessage   string
	LastPackage     string
	RemainingTime   int64
}

func (c *Client) liveUpdateObjectPath(ctx context.Context) (dbus.ObjectPath, bool) {
	paths, err := c.GetTransactionList(ctx)
	if err != nil {
		return "", false
	}
	var refreshPath dbus.ObjectPath
	haveRefresh := false
	for _, path := range paths {
		role, ok := c.transactionUintProperty(ctx, path, "Role")
		if !ok {
			continue
		}
		if status, statOk := c.transactionUintProperty(ctx, path, "Status"); statOk && (status == StatusFinished || status == StatusCanceled) {
			continue
		}
		switch role {
		case RoleUpdatePackages:
			return path, true
		case RoleRefreshCache:
			if !haveRefresh {
				refreshPath, haveRefresh = path, true
			}
		}
	}
	return refreshPath, haveRefresh
}

// UpdateSnapshot returns a snapshot of the running package-update transaction,
// or nil when no update is currently in flight.
func (c *Client) UpdateSnapshot(ctx context.Context) *LiveUpdateSnapshot {
	path, ok := c.liveUpdateObjectPath(ctx)
	if !ok {
		return nil
	}
	snapshot := &LiveUpdateSnapshot{TransactionPath: string(path)}
	if value, ok := c.transactionUintProperty(ctx, path, "Percentage"); ok {
		snapshot.Percentage = value
	}
	if value, ok := c.transactionBoolProperty(ctx, path, "AllowCancel"); ok {
		snapshot.AllowCancel = value
	}
	if value, ok := c.transactionUintProperty(ctx, path, "Status"); ok {
		snapshot.Status = value
		snapshot.StatusMessage = StatusMessage(value)
	}
	if value, ok := c.transactionStringProperty(ctx, path, "LastPackage"); ok {
		snapshot.LastPackage = value
	}
	if value, ok := c.transactionIntProperty(ctx, path, "RemainingTime"); ok {
		snapshot.RemainingTime = value
	}
	return snapshot
}

// CancelActiveUpdate cancels the running package-update transaction, if any.
// It reports whether a transaction was found and cancelled.
func (c *Client) CancelActiveUpdate(ctx context.Context) (bool, error) {
	path, ok := c.liveUpdateObjectPath(ctx)
	if !ok {
		return false, nil
	}
	if err := c.cancelTransaction(path); err != nil {
		return true, err
	}
	return true, nil
}

func (c *Client) transactionBoolProperty(ctx context.Context, path dbus.ObjectPath, name string) (bool, bool) {
	obj := c.conn.Object(dbusDest, path)
	var value bool
	if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, transactionIface, name).Store(&value); err != nil {
		return false, false
	}
	return value, true
}

func (c *Client) transactionIntProperty(ctx context.Context, path dbus.ObjectPath, name string) (int64, bool) {
	obj := c.conn.Object(dbusDest, path)
	var value int64
	if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, transactionIface, name).Store(&value); err != nil {
		return 0, false
	}
	return value, true
}

// statusMessages covers the PackageKit statuses surfaced during an
// update; unknown values fall back to a generic label.
var statusMessages = map[uint32]string{
	StatusWait:           "Waiting",
	StatusWaitingForLock: "Waiting for another package operation",
	2:                    "Downloading",
	3:                    "Querying",
	5:                    "Removing",
	6:                    "Refreshing",
	7:                    "Downgrading",
	8:                    "Downloading",
	9:                    "Installing",
	10:                   "Updating",
	11:                   "Cleaning up",
	12:                   "Resolving dependencies",
	14:                   "Checking signatures",
	16:                   "Committing",
}

// StatusMessage maps a PackageKit status enum to a human label.
func StatusMessage(status uint32) string {
	switch status {
	case StatusFinished:
		return "Finished"
	case StatusCanceled:
		return "Canceled"
	}
	if message, ok := statusMessages[status]; ok {
		return message
	}
	return "Updating"
}

// HistoryEntry is one past update transaction: a timestamp plus the packages
// it touched (name -> version).
type HistoryEntry struct {
	Time     int64             `json:"time"`
	Packages map[string]string `json:"packages"`
}

const maxHistoryEntries = 20

// GetOldTransactions returns past update-package transactions, newest first:
// filter to ROLE_UPDATE_PACKAGES and parse the "action\tpackage-id" data
// lines.
func (c *Client) GetOldTransactions(ctx context.Context) ([]HistoryEntry, error) {
	transactionPath, err := c.createTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer c.removeTransactionSignal(transactionPath)

	sigChan := make(chan *dbus.Signal, 64)
	c.conn.Signal(sigChan)
	defer c.conn.RemoveSignal(sigChan)
	_ = c.conn.AddMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))
	defer c.conn.RemoveMatchSignal(dbus.WithMatchInterface(transactionIface), dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionPath)))

	history := make([]HistoryEntry, 0, maxHistoryEntries)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(done)
		for sig := range sigChan {
			if sig.Path != transactionPath {
				continue
			}
			if sig.Name == transactionIface+".Finished" {
				// PackageKit emits Finished once old-transaction enumeration
				// completes, even when the list is empty.
				close(finished)
				return
			}
			if sig.Name != transactionIface+".Transaction" || len(sig.Body) < 5 {
				continue
			}
			// Body: (objectPath, timeSpec, succeeded, role, duration, data); we
			// keep the transaction list regardless of success.
			timeSpec, ok := sig.Body[1].(string)
			if !ok {
				continue
			}
			role, ok := sig.Body[3].(uint32)
			if !ok {
				continue
			}
			data, ok := sig.Body[4].(string)
			if !ok {
				continue
			}
			parsed, ok := parseHistoryTimestamp(timeSpec)
			if !ok || role != RoleUpdatePackages {
				continue
			}
			entry := HistoryEntry{Time: parsed.UnixMilli(), Packages: map[string]string{}}
			for _, line := range strings.Split(data, "\n") {
				fields := strings.Split(strings.TrimSpace(line), "\t")
				if len(fields) < 2 {
					continue
				}
				idFields := strings.Split(fields[1], ";")
				if len(idFields) < 2 || idFields[0] == "" {
					continue
				}
				entry.Packages[idFields[0]] = idFields[1]
			}
			if len(entry.Packages) > 0 {
				history = append(history, entry)
				if len(history) >= maxHistoryEntries {
					return
				}
			}
		}
	}()

	obj := c.conn.Object(dbusDest, transactionPath)
	call := obj.CallWithContext(ctx, transactionIface+".GetOldTransactions", 0, uint32(0))
	if call.Err != nil {
		return nil, call.Err
	}
	finalize := func() []HistoryEntry {
		// Newest first (PK reports ascending).
		for left, right := 0, len(history)-1; left < right; left, right = left+1, right-1 {
			history[left], history[right] = history[right], history[left]
		}
		return history
	}
	select {
	case <-finished:
		return finalize(), nil
	case <-done:
		// Goroutine exited early: the history window was filled.
		return finalize(), nil
	case <-ctx.Done():
		_ = c.cancelTransaction(transactionPath)
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		_ = c.cancelTransaction(transactionPath)
		return nil, errors.New("GetOldTransactions timeout")
	}
}

// parseHistoryTimestamp handles the timezone formats PackageKit emits,
// including the short offset form ("2026-01-29T12:57:49.112827-08") that
// neither Firefox nor Chromium parse but Go does after normalisation.
func parseHistoryTimestamp(timeSpec string) (time.Time, bool) {
	trimmed := strings.TrimSpace(timeSpec)
	if trimmed == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return parsed, true
	}
	// Normalise short offsets (-08) to RFC3339 (-08:00).
	if len(trimmed) > 3 {
		tail := trimmed[len(trimmed)-3:]
		if (tail[0] == '-' || tail[0] == '+') && tail[1] >= '0' && tail[1] <= '9' && tail[2] >= '0' && tail[2] <= '9' {
			if parsed, err := time.Parse(time.RFC3339Nano, trimmed+":00"); err == nil {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}
