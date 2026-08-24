package packagekit

import (
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// maxActionLogEntries bounds the per-transaction action log so a huge update
// cannot grow memory without limit.
const maxActionLogEntries = 500

// ActionLogEntry is one Package signal observed during an update transaction:
// what happened to which package.
type ActionLogEntry struct {
	Status      uint32    `json:"status"`
	StatusLabel string    `json:"statusLabel"`
	PackageID   string    `json:"packageId"`
	Timestamp   time.Time `json:"timestamp"`
}

// TransactionWatcher records Package signals emitted by PackageKit
// transactions so the dashboard can render an update log even though the
// update itself runs inside sessiond or another client. It is safe for
// concurrent use and tolerates being started when PackageKit is absent
// (signals simply never arrive).
type TransactionWatcher struct {
	conn *dbus.Conn

	mu      sync.Mutex
	logs    map[dbus.ObjectPath][]ActionLogEntry
	order   []dbus.ObjectPath
	started bool
	stop    chan struct{}
	stopd   chan struct{}
}

// NewTransactionWatcher connects to the system bus and prepares the watcher.
func NewTransactionWatcher() (*TransactionWatcher, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &TransactionWatcher{conn: conn, logs: make(map[dbus.ObjectPath][]ActionLogEntry)}, nil
}

// Start subscribes to transaction signals. Calling Start more than once is a
// no-op; Close releases the bus connection and stops collection.
func (watcher *TransactionWatcher) Start() {
	watcher.mu.Lock()
	if watcher.started {
		watcher.mu.Unlock()
		return
	}
	watcher.started = true
	watcher.stop = make(chan struct{})
	watcher.stopd = make(chan struct{})
	watcher.mu.Unlock()

	if err := watcher.conn.AddMatchSignal(
		dbus.WithMatchInterface(transactionIface),
		dbus.WithMatchPathNamespace(dbus.ObjectPath(transactionsPath)),
	); err != nil {
		close(watcher.stopd)
		return
	}
	sigChan := make(chan *dbus.Signal, 128)
	watcher.conn.Signal(sigChan)

	go func() {
		defer close(watcher.stopd)
		defer watcher.conn.RemoveSignal(sigChan)
		for {
			select {
			case <-watcher.stop:
				return
			case sig, ok := <-sigChan:
				if !ok {
					return
				}
				if sig.Name != transactionIface+".Package" || len(sig.Body) < 2 {
					continue
				}
				status, okStatus := sig.Body[0].(uint32)
				packageID, okID := sig.Body[1].(string)
				if !okStatus || !okID {
					continue
				}
				watcher.record(sig.Path, ActionLogEntry{
					Status:      status,
					StatusLabel: StatusMessage(status),
					PackageID:   packageID,
					Timestamp:   time.Now().UTC(),
				})
			}
		}
	}()
}

func (watcher *TransactionWatcher) record(path dbus.ObjectPath, entry ActionLogEntry) {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	entries := append(watcher.logs[path], entry)
	if len(entries) > maxActionLogEntries {
		entries = entries[len(entries)-maxActionLogEntries:]
	}
	if _, exists := watcher.logs[path]; !exists {
		watcher.order = append(watcher.order, path)
		// Keep at most the two most recent transactions' logs (current plus the
		// one that just finished).
		for len(watcher.order) > 2 {
			delete(watcher.logs, watcher.order[0])
			watcher.order = watcher.order[1:]
		}
	}
	watcher.logs[path] = entries
}

// ActionLog returns the recorded Package signals for a transaction path.
func (watcher *TransactionWatcher) ActionLog(path string) []ActionLogEntry {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	entries := watcher.logs[dbus.ObjectPath(path)]
	out := make([]ActionLogEntry, len(entries))
	copy(out, entries)
	return out
}

// LatestLog returns the most recent transaction's log, preferring the given
// live path when it has entries.
func (watcher *TransactionWatcher) LatestLog(livePath string) []ActionLogEntry {
	if entries := watcher.ActionLog(livePath); len(entries) > 0 {
		return entries
	}
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	if len(watcher.order) == 0 {
		return []ActionLogEntry{}
	}
	entries := watcher.logs[watcher.order[len(watcher.order)-1]]
	out := make([]ActionLogEntry, len(entries))
	copy(out, entries)
	return out
}

// Close unsubscribes and releases the connection.
func (watcher *TransactionWatcher) Close() {
	watcher.mu.Lock()
	started := watcher.started
	if started && watcher.stop != nil {
		select {
		case <-watcher.stop:
		default:
			close(watcher.stop)
		}
	}
	watcher.mu.Unlock()
	if started {
		<-watcher.stopd
	}
	if watcher.conn != nil {
		_ = watcher.conn.Close()
	}
}
