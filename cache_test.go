package weakcache_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/mgnsk/weakcache"
	. "github.com/onsi/gomega"
)

func TestGCEviction(t *testing.T) {
	g := NewWithT(t)

	cache := weakcache.New(10 * time.Millisecond)
	defer cache.Close()

	// Fetch an item with 0 minTTL and 0 maxTTL.
	rec, _ := cache.Fetch("key", 0, 0, func() (interface{}, error) {
		return "value", nil
	})

	g.Expect(rec.Value).To(Equal("value"))

	g.Expect(cache.Len()).To(Equal(1))

	runtime.KeepAlive(rec)

	runtime.GC()

	// Record was evicted by GC due to being unreachable.
	// "Eventually" is used because there are 2 goroutines involved
	// in evicting the record.
	g.Eventually(func() int {
		return cache.Len()
	}).Should(Equal(0))
}

func TestMinTTL(t *testing.T) {
	g := NewWithT(t)

	// minTTL specifies how long the item will survive being unreferenced.

	cache := weakcache.New(10 * time.Millisecond)
	defer cache.Close()

	// Fetch an item with minTTL set.
	rec1, _ := cache.Fetch("key", 100*time.Millisecond, 0, func() (interface{}, error) {
		return "value", nil
	})

	g.Expect(rec1.Value).To(Equal("value"))

	g.Expect(cache.Len()).To(Equal(1))

	runtime.KeepAlive(rec1)

	// rec1 is now unreachable.

	runtime.GC()

	// rec1 was collected, but the cache record will be kept alive for at least minTTL.

	time.Sleep(50 * time.Millisecond)

	g.Expect(cache.Len()).To(Equal(1))

	// Obtain a new reference to the cache record.
	rec2, _ := cache.Fetch("key", 100*time.Millisecond, 0, func() (interface{}, error) {
		// The record should exist in the cache.
		panic("unexpected fetch fallback")
	})
	_ = rec2

	// rec2 is now unreachable, the GC will collect it and decrement the reference count for the cache record.
	runtime.GC()

	// rec2 was collected and the last reference time of the record was updated.
	// We should now have 100ms left until the item gets evicted.

	time.Sleep(90 * time.Millisecond)

	g.Expect(cache.Len()).To(Equal(1))

	g.Eventually(func() int {
		return cache.Len()
	}).Should(Equal(0))
}

func TestMaxTTL(t *testing.T) {
	g := NewWithT(t)

	cache := weakcache.New(10 * time.Millisecond)
	defer cache.Close()

	// Fetch an item with minTTL set.
	rec1, _ := cache.Fetch("key", 0, 100*time.Millisecond, func() (interface{}, error) {
		return "value", nil
	})

	g.Expect(rec1.Value).To(Equal("value"))

	g.Expect(cache.Len()).To(Equal(1))

	time.Sleep(120 * time.Millisecond)

	// If a record with the same key exists whose maxTTL has passed,
	// or if it has not had any references within the last minTTL,
	// it will be evicted during Fetch.
	// Otherwise, unreachable records will be evicted automatically
	// with respect to minTTL and maxTTL.

	// Fetch the same item again (evicting the previous).
	// Expect it to call fetch due to previous record expired.
	fetchCalled := false
	rec2, _ := cache.Fetch("key", 0, 0, func() (interface{}, error) {
		fetchCalled = true
		return "new value", nil
	})

	g.Expect(fetchCalled).To(BeTrue())

	g.Expect(cache.Len()).To(Equal(1))

	g.Expect(rec2.Value).To(Equal("new value"))

	// We are keeping the pointers reachable to keep them from being
	// evicted and to only test maxTTL.
	runtime.KeepAlive(rec2)
	runtime.KeepAlive(rec1)
}
