package sessiond

import (
	"context"
	"time"

	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/platform"
)

const (
	hostMetricRetention = 24 * time.Hour
	hostMetricInterval  = time.Minute
)

type hostRuntime struct {
	sampler         *metrics.Sampler
	processTracker  *platform.ProcessTracker
	certificatePath string
	updates         *platform.UpdateService
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
	runtime.sampler.Run(ctx, 0)
}
func (runtime *hostRuntime) close() {}
func (runtime *hostRuntime) updateObservation(_ context.Context) platform.UpdateObservation {
	if runtime == nil || runtime.updates == nil {
		return platform.UpdateObservation{Progress: platform.UpdateProgress{Phase: "idle", Percent: -1, Message: "No update is running."}, Output: []platform.UpdateOutput{}}
	}
	return runtime.updates.Snapshot()
}
