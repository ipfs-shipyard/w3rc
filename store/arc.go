package store

import (
	"context"
	"fmt"

	"github.com/dgraph-io/ristretto"
)

type CachingStore struct {
	ristretto.Cache
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
	c.Cache.Set(key, content, 0)
	return nil
}
