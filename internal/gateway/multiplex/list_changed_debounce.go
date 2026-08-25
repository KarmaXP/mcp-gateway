package multiplex

import (
	"sync"
	"time"
)

type listChangedDebouncer struct {
	mu         sync.Mutex
	delay      time.Duration
	timer      *time.Timer
	generation uint64
}

func (d *listChangedDebouncer) schedule(refresh func()) {
	if d.delay <= 0 {
		refresh()
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
	}
	d.generation++
	generation := d.generation
	d.timer = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		if generation != d.generation {
			d.mu.Unlock()
			return
		}
		d.timer = nil
		d.mu.Unlock()
		refresh()
	})
}
