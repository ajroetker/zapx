package zap

import (
	"sync"
	"testing"
)

// TestInvertedIndexCacheClearConcurrent verifies that calling Clear
// concurrently with insertLOCKED does not panic due to a nil map write.
// Before the fix, Clear set cache = nil which caused "assignment to
// entry in nil map" panics when insertLOCKED ran concurrently.
func TestInvertedIndexCacheClearConcurrent(t *testing.T) {
	cache := newInvertedIndexCache()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			cache.Clear()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			cache.m.Lock()
			cache.insertLOCKED(uint16(i%64), nil)
			cache.m.Unlock()
		}
	}()

	wg.Wait()
}

// TestSynonymIndexCacheClearConcurrent verifies the same fix for the
// synonym index cache.
func TestSynonymIndexCacheClearConcurrent(t *testing.T) {
	cache := newSynonymIndexCache()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			cache.Clear()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			cache.m.Lock()
			cache.insertLOCKED(uint16(i%64), nil, nil)
			cache.m.Unlock()
		}
	}()

	wg.Wait()
}
