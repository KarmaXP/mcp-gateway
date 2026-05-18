package ratelimit

import (
	"container/list"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type bucketMap struct {
	mu         sync.Mutex
	maxBuckets int
	idleTTL    time.Duration
	byKey      map[string]*bucketEntry
	lru        *list.List
	elems      map[string]*list.Element
}

func newBucketMap(maxBuckets int, idleTTL time.Duration) *bucketMap {
	if maxBuckets <= 0 {
		maxBuckets = defaultMaxBuckets
	}
	return &bucketMap{
		maxBuckets: maxBuckets,
		idleTTL:    idleTTL,
		byKey:      make(map[string]*bucketEntry),
		lru:        list.New(),
		elems:      make(map[string]*list.Element),
	}
}

func (m *bucketMap) len() int {
	return len(m.byKey)
}

func (m *bucketMap) contains(key string) bool {
	_, ok := m.byKey[key]
	return ok
}

func (m *bucketMap) allow(key string, lim rate.Limit, burst int, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.evictStaleLocked(now)
	e, ok := m.byKey[key]
	if !ok {
		m.evictToCapLocked()
		e = &bucketEntry{lim: rate.NewLimiter(lim, burst), lastSeen: now}
		m.byKey[key] = e
		m.elems[key] = m.lru.PushFront(key)
	} else {
		e.lastSeen = now
		m.touchLocked(key)
	}
	return e.lim.Allow()
}

func (m *bucketMap) evictStaleLocked(now time.Time) {
	for key, e := range m.byKey {
		if now.Sub(e.lastSeen) > m.idleTTL {
			m.removeKeyLocked(key)
		}
	}
}

func (m *bucketMap) evictToCapLocked() {
	for len(m.byKey) >= m.maxBuckets {
		back := m.lru.Back()
		if back == nil {
			return
		}
		key, _ := back.Value.(string)
		m.removeKeyLocked(key)
	}
}

func (m *bucketMap) touchLocked(key string) {
	if el, ok := m.elems[key]; ok {
		m.lru.MoveToFront(el)
	}
}

func (m *bucketMap) removeKeyLocked(key string) {
	if el, ok := m.elems[key]; ok {
		m.lru.Remove(el)
		delete(m.elems, key)
	}
	delete(m.byKey, key)
}

func (m *bucketMap) sweepStale(now time.Time) {
	m.mu.Lock()
	m.evictStaleLocked(now)
	m.mu.Unlock()
}

const idleTTLEvictionDivisor = 2

func evictionInterval(idleTTL time.Duration) time.Duration {
	interval := idleTTL / idleTTLEvictionDivisor
	if interval < time.Second {
		return time.Second
	}
	const maxInterval = 5 * time.Minute
	if interval > maxInterval {
		return maxInterval
	}
	return interval
}
