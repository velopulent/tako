package sessiond

import (
	"context"
	"testing"
	"time"
)

func TestHostRuntimeSharesMetricHistoryAndSubscribers(t *testing.T) {
	runtime := newHostRuntime(time.Second, time.Hour)
	defer runtime.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runtime.run(ctx)

	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		if _, ok := runtime.sampler.Current(); ok {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("sessiond collector did not publish an initial sample")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if len(runtime.sampler.HistorySince(time.Now().Add(-time.Hour), 100)) == 0 {
		t.Fatal("shared sessiond history is empty after initial collection")
	}

	first, unsubscribeFirst := runtime.sampler.SubscribeEvery(time.Second)
	second, unsubscribeSecond := runtime.sampler.SubscribeEvery(time.Second)
	defer unsubscribeSecond()
	firstClosed := false
	defer func() {
		if !firstClosed {
			unsubscribeFirst()
		}
	}()

	select {
	case <-first:
	case <-time.After(3 * time.Second):
		t.Fatal("first metrics subscriber did not receive a sample")
	}
	select {
	case <-second:
	case <-time.After(3 * time.Second):
		t.Fatal("second metrics subscriber did not receive a sample")
	}

	unsubscribeFirst()
	firstClosed = true
	select {
	case _, open := <-first:
		if open {
			t.Fatal("unsubscribed metrics channel remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("unsubscribed metrics channel was not closed")
	}
	select {
	case _, open := <-second:
		if !open {
			t.Fatal("one subscriber was closed when another unsubscribed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second metrics subscriber stopped receiving samples")
	}
}
