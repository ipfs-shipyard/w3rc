package store

import (
	"context"
	"fmt"

	"github.com/dgraph-io/ristretto"
)

type CachingStore struct {
	ristretto.Cache
}

const AVG_SIZE = 1000

func NewCachingStore(size uint64) *CachingStore {
	cfg := ristretto.Config{
		NumCounters: int64(size / AVG_SIZE),
		MaxCost:     int64(size),
		BufferItems: 64,
		Metrics:     false,
		OnEvict:     func(key, conflict uint64, value interface{}, cost int64) {},
	}
	cache, err := ristretto.NewCache(&cfg)
	if err != nil {
		return nil
	}
	cs := CachingStore{
		Cache: *cache,
	}
	return &cs
}

func (c *CachingStore) Has(ctx context.Context, key string) (bool, error) {
	_, has := c.Cache.Get(key)
	return has, nil
}

func (c *CachingStore) Get(ctx context.Context, key string) ([]byte, error) {
	val, _ := c.Cache.Get(key)
	vb, ok := val.([]byte)
	if ok {
		return vb, nil
	}
	return nil, fmt.Errorf("404")
}

func (c *CachingStore) Put(ctx context.Context, key string, content []byte) error {
	c.Cache.Set(key, content, int64(len(content)))
	return nil
}
