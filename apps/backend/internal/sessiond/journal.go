package sessiond

import (
	"sync"
)

type journalSlotKey struct {
	uid    int
	follow bool
}

var journalSlots = journalSlotLimiter{counts: make(map[journalSlotKey]int)}

type journalSlotLimiter struct {
	sync.Mutex
	counts map[journalSlotKey]int
	total  int
}

func (l *journalSlotLimiter) acquire(uid int, follow bool) (func(), bool) {
	l.Lock()
	defer l.Unlock()
	key := journalSlotKey{uid, follow}
	limit := 8
	if follow {
		limit = 4
	}
	if l.total >= 64 || l.counts[key] >= limit {
		return nil, false
	}
	l.counts[key]++
	l.total++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.Lock()
			defer l.Unlock()
			l.counts[key]--
			l.total--
			if l.counts[key] == 0 {
				delete(l.counts, key)
			}
		})
	}, true
}
