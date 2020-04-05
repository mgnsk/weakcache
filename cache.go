package weakcache

import (
	"hash/maphash"
	"runtime"
	"sync"
	"time"
)

// Record is a reference-counted cache record.
type Record struct {
	Value     interface{}
	minTTL    int64
	expires   int64
	refs      uint
	lastUnref int64
}

// isExpired reports if the record has expired or
// has been unreferenced for too long.
func (r Record) isExpired(now int64) bool {
	// The record has not been referenced for at least r.minTTL duration.
	if r.lastUnref > 0 && r.lastUnref+int64(r.minTTL) < now {
		return true
	}
	if r.expires > 0 && r.expires < now {
		return true
	}
	return false
}

type recordMap map[uint64]Record

type fetch func() (interface{}, error)

// Cache is a reference-counting cache which lets keys and values
// that have no reference outside of the cache be garbage collected.
type Cache struct {
	mu          sync.Mutex
	gcInterval  time.Duration
	reachable   recordMap
	unreachable recordMap
	seed        maphash.Seed
	quit        chan struct{}
}

// New creates an empty cache.
func New(gcInterval time.Duration) *Cache {
	c := &Cache{
		gcInterval:  gcInterval,
		reachable:   make(recordMap),
		unreachable: make(recordMap),
		seed:        maphash.MakeSeed(),
		quit:        make(chan struct{}),
	}

	go c.gcLoop()

	return c
}

// Fetch gets or sets a record. It calls fetch as a fallback on cache miss.
// minTTL specifies how long the record will survive without being referenced.
// maxTTL specifies the maximum lifetime of the record.
func (c *Cache) Fetch(key string, minTTL, maxTTL time.Duration, fetch fetch) (*Record, error) {
	index := c.index(key)

	// Acquire a unique pointer to the record. When the pointer gets garbage collected,
	// the reference count for the record will be decremented.
	rec, err := c.fetch(index, minTTL, maxTTL, fetch)
	if err != nil {
		return nil, err
	}

	runtime.SetFinalizer(rec, func(_ interface{}) {
		go c.unref(index)
	})

	return rec, nil
}

// Len returns the number of cached items.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reachable) + len(c.unreachable)
}

// Close stops the cache GC loop.
func (c *Cache) Close() {
	close(c.quit)
}

func (c *Cache) index(key string) uint64 {
	var h maphash.Hash
	h.SetSeed(c.seed)
	h.WriteString(key)
	return h.Sum64()
}

func (c *Cache) gcLoop() {
	ticker := time.NewTicker(c.gcInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.quit:
			return
		case now := <-ticker.C:
			nowNano := now.UnixNano()
			c.mu.Lock()
			// Clean up unreachable records,
			for index, rec := range c.unreachable {
				if rec.isExpired(nowNano) {
					delete(c.unreachable, index)
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *Cache) fetch(index uint64, minTTL, maxTTL time.Duration, fetch fetch) (*Record, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	get := func() *Record {
		if rec, ok := c.unreachable[index]; ok {
			// An unreachable record was found, make it reachable later.
			delete(c.unreachable, index)
			if rec.isExpired(now.UnixNano()) {
				return nil
			}
			return &rec
		} else if rec, ok = c.reachable[index]; ok {
			if rec.isExpired(now.UnixNano()) {
				delete(c.reachable, index)
				return nil
			}
			// A reachable record was found.
			return &rec
		}
		return nil
	}

	rec := get()
	if rec == nil {
		// Create a new record.
		value, err := fetch()
		if err != nil {
			return nil, err
		}
		rec = &Record{
			Value:  value,
			minTTL: int64(minTTL),
		}
		if maxTTL > 0 {
			rec.expires = now.Add(maxTTL).UnixNano()
		}
	}

	rec.refs++

	// Store a value in the map. The pointer is returned only to the caller
	// so that the caller triggers a finalizer when the pointer becomes unreachable.
	c.reachable[index] = *rec

	return rec, nil
}

// unref is called when a reference to a cache record gets garbage collected.
func (c *Cache) unref(index uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	rec, ok := c.reachable[index]
	if !ok {
		// The record probably expired during fetch
		// while having other live references.
		return
	}

	// Decrease reference count for the record.
	rec.refs--
	if rec.refs > 0 {
		// Record has other live references.
		c.reachable[index] = rec
	} else {
		// No references, move to unreachable map.
		delete(c.reachable, index)
		// Mark the last unref time so that the record would survive
		// being unreachable until at least minTTL duration has passed.
		rec.lastUnref = time.Now().UnixNano()
		c.unreachable[index] = rec
	}
}
