// Package idle coordinates clean shutdown for socket-activated services.
package idle

import (
	"sync"
	"time"
)

// Tracker closes Done after a full idle interval with no active blockers.
// It is inert until Start is called, allowing direct-started processes to
// remain resident while socket-activated processes opt into idle shutdown.
type Tracker struct {
	mu       sync.Mutex
	timeout  time.Duration
	last     time.Time
	blockers int
	busy     map[string]bool
	started  bool
	stopped  bool
	timer    *time.Timer
	done     chan struct{}
	once     sync.Once
}

func New(timeout time.Duration) *Tracker {
	return &Tracker{timeout: timeout, busy: make(map[string]bool), done: make(chan struct{})}
}

// SetBusy maintains one named blocker for stateful subsystems such as session
// stores and job queues.
func (tracker *Tracker) SetBusy(name string, busy bool) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.stopped || tracker.busy[name] == busy {
		return
	}
	tracker.busy[name] = busy
	if busy {
		tracker.blockers++
		tracker.last = time.Now()
	} else if tracker.blockers > 0 {
		tracker.blockers--
	}
	tracker.scheduleLocked()
}

func (tracker *Tracker) Done() <-chan struct{} {
	return tracker.done
}

func (tracker *Tracker) Start() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.started || tracker.stopped || tracker.timeout <= 0 {
		return
	}
	tracker.started = true
	tracker.last = time.Now()
	tracker.scheduleLocked()
}

func (tracker *Tracker) Stop() {
	tracker.mu.Lock()
	tracker.stopped = true
	if tracker.timer != nil {
		tracker.timer.Stop()
		tracker.timer = nil
	}
	tracker.mu.Unlock()
}

// Activity resets the idle interval.
func (tracker *Tracker) Activity() {
	tracker.mu.Lock()
	if !tracker.stopped {
		tracker.last = time.Now()
		tracker.scheduleLocked()
	}
	tracker.mu.Unlock()
}

// Block marks work that must finish before idle shutdown. If activity was
// already idle for the full interval, releasing the final blocker fires now.
func (tracker *Tracker) Block() func() {
	tracker.mu.Lock()
	if !tracker.stopped {
		tracker.blockers++
		tracker.last = time.Now()
		tracker.scheduleLocked()
	}
	tracker.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			tracker.mu.Lock()
			if tracker.blockers > 0 {
				tracker.blockers--
			}
			tracker.scheduleLocked()
			tracker.mu.Unlock()
		})
	}
}

func (tracker *Tracker) scheduleLocked() {
	if tracker.timer != nil {
		tracker.timer.Stop()
		tracker.timer = nil
	}
	if !tracker.started || tracker.stopped || tracker.timeout <= 0 || tracker.blockers > 0 {
		return
	}
	delay := time.Until(tracker.last.Add(tracker.timeout))
	if delay < 0 {
		delay = 0
	}
	tracker.timer = time.AfterFunc(delay, tracker.fire)
}

func (tracker *Tracker) fire() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if !tracker.started || tracker.stopped || tracker.blockers > 0 {
		return
	}
	if remaining := time.Until(tracker.last.Add(tracker.timeout)); remaining > 0 {
		tracker.timer = time.AfterFunc(remaining, tracker.fire)
		return
	}
	tracker.stopped = true
	tracker.timer = nil
	tracker.once.Do(func() { close(tracker.done) })
}
