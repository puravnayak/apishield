package cache

import (
	"sync"
	"time"
)

type item struct {
	value      interface{}
	expiration int64
}

type Shard struct {
	mu    sync.RWMutex
	items map[string]item
}

type ShardedCache struct {
	shards []*Shard
}

func NewShardedCache() *ShardedCache {
	c := &ShardedCache{
		shards: make([]*Shard, 256),
	}
	for i := 0; i < 256; i++ {
		c.shards[i] = &Shard{
			items: make(map[string]item),
		}
	}
	return c
}

func fnv1a(key string) uint32 {
	var hash uint32 = 2166136261
	const prime uint32 = 16777619
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= prime
	}
	return hash
}

func (c *ShardedCache) getShard(key string) *Shard {
	idx := fnv1a(key) % 256
	return c.shards[idx]
}

func (c *ShardedCache) Get(key string) (interface{}, bool) {
	s := c.getShard(key)
	s.mu.RLock()
	defer s.mu.RUnlock()

	it, exists := s.items[key]
	if !exists {
		return nil, false
	}
	if it.expiration > 0 && time.Now().UnixNano() > it.expiration {
		return nil, false
	}
	return it.value, true
}

func (c *ShardedCache) Set(key string, val interface{}, ttl time.Duration) {
	s := c.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	var exp int64
	if ttl > 0 {
		exp = time.Now().Add(ttl).UnixNano()
	}
	s.items[key] = item{
		value:      val,
		expiration: exp,
	}
}

func (c *ShardedCache) Delete(key string) {
	s := c.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}
