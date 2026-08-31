package sessiond

import (
	"context"
	"sync"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/packagekit"
	"github.com/velopulent/tako/internal/platform"
)

const (
	hostMetricRetention = 24 * time.Hour
	hostMetricInterval  = time.Minute
)

// hostRuntime owns host-wide readers that must outlive an individual HTTP
// request. It lives in sessiond, never in the network gateway.
type hostRuntime struct {
	sampler         *metrics.Sampler
	processTracker  *platform.ProcessTracker
	certificatePath string

	updateMu      sync.Mutex
	updateClient  *packagekit.Client
	updateWatcher *packagekit.TransactionWatcher
}

func newHostRuntime(settings ...time.Duration) *hostRuntime {
	defaultInterval, retention := hostMetricInterval, hostMetricRetention
	if len(settings) > 0 && settings[0] >= time.Second {
		defaultInterval = settings[0]
	}
	if len(settings) > 1 && settings[1] > 0 && settings[1] <= hostMetricRetention {
		retention = settings[1]
	}
	capacity := int(retention/hostMetricInterval) + 1
	if capacity < 2 {
		capacity = 2
	}
	sampler := metrics.NewSampler(capacity)
	sampler.Configure(defaultInterval, retention)
	return &hostRuntime{sampler: sampler, processTracker: platform.NewProcessTracker()}
}

func (runtime *hostRuntime) run(ctx context.Context) {
	if runtime == nil || runtime.sampler == nil {
		return
	}
	// Configure already installed the deployment's default interval. Passing a
	// non-zero value here would silently replace it with the legacy one-minute
	// interval.
	runtime.sampler.Run(ctx, 0)
}

func (runtime *hostRuntime) close() {
	if runtime == nil {
		return
	}
	runtime.updateMu.Lock()
	client, watcher := runtime.updateClient, runtime.updateWatcher
	runtime.updateClient, runtime.updateWatcher = nil, nil
	runtime.updateMu.Unlock()
	if watcher != nil {
		watcher.Close()
	}
	if client != nil {
		client.Close()
	}
}

func (runtime *hostRuntime) updateClientFor(ctx context.Context) *packagekit.Client {
	runtime.updateMu.Lock()
	defer runtime.updateMu.Unlock()
	if runtime.updateClient != nil {
		return runtime.updateClient
	}
	client, err := packagekit.New()
	if err != nil || !client.Detect(ctx) {
		if client != nil {
			client.Close()
		}
		return nil
	}
	runtime.updateClient = client
	return client
}

func (runtime *hostRuntime) updateWatcherFor() *packagekit.TransactionWatcher {
	runtime.updateMu.Lock()
	defer runtime.updateMu.Unlock()
	if runtime.updateWatcher != nil {
		return runtime.updateWatcher
	}
	watcher, err := packagekit.NewTransactionWatcher()
	if err != nil {
		return nil
	}
	watcher.Start()
	runtime.updateWatcher = watcher
	return watcher
}

func (runtime *hostRuntime) updateObservation(ctx context.Context) auth.UpdateObservation {
	result := auth.UpdateObservation{Live: platform.InactiveUpdateLive(), Log: []packagekit.ActionLogEntry{}}
	observeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client := runtime.updateClientFor(observeCtx)
	var snapshot *packagekit.LiveUpdateSnapshot
	if client != nil {
		snapshot = client.UpdateSnapshot(observeCtx)
		result.Live = platform.UpdateLiveFromSnapshot(snapshot)
	}
	if watcher := runtime.updateWatcherFor(); watcher != nil {
		path := ""
		if snapshot != nil {
			path = snapshot.TransactionPath
		}
		result.Log = watcher.LatestLog(path)
	}
	return result
}
