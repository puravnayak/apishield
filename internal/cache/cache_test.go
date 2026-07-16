package cache

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestShardedCacheBasic(t *testing.T) {
	c := NewShardedCache()

	c.Set("foo", "bar", 10*time.Millisecond)

	val, found := c.Get("foo")
	if !found {
		t.Fatal("expected key to exist")
	}
	if val != "bar" {
		t.Errorf("expected bar, got %v", val)
	}

	time.Sleep(15 *time.Millisecond)

	_, found = c.Get("foo")
	if found {
		t.Error("expected key to be expired")
	}
}

func TestShardedCacheDelete(t *testing.T) {
	c := NewShardedCache()

	c.Set("foo", "bar", 0)
	c.Delete("foo")

	_, found := c.Get("foo")
	if found {
		t.Error("expected key to be deleted")
	}
}

func TestShardedCacheConcurrency(t *testing.T) {
	c := NewShardedCache()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "key-" + strconv.Itoa(id)
			c.Set(key, id, time.Hour)
			val, found := c.Get(key)
			if !found || val != id {
				t.Errorf("failed concurrent set/get for %s: %v", key, val)
			}
		}(i)
	}

	wg.Wait()
}
