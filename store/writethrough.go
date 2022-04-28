package store

import (
	"context"

	"github.com/ipld/go-ipld-prime/storage"
)

// A muxer for a caching store that can be used by multiple concurrent link systems.
// Items stored in a given session will be retained consistently for the duration of the system.
// otherwise, items may be garbage collected using an LRU cache.

type WriteThroughStore struct {
	backingReader storage.ReadableStorage
	backingWriter storage.WritableStorage
	cache         map[string][]byte
}

func NewWriteThroughStore(backingReader storage.ReadableStorage, backingWriter storage.WritableStorage) (storage.ReadableStorage, storage.WritableStorage) {
	wts := WriteThroughStore{
		backingReader: backingReader,
		backingWriter: backingWriter,
		cache:         make(map[string][]byte),
	}
	return &wts, &wts
}

func (w *WriteThroughStore) Has(ctx context.Context, key string) (bool, error) {
	_, cached := w.cache[key]
	if cached {
		return true, nil
	}
	b, err := w.backingReader.Get(ctx, key)
	if err == nil {
		w.cache[key] = b
		return true, nil
	}
	return false, err
}

func (w *WriteThroughStore) Get(ctx context.Context, key string) ([]byte, error) {
	content, cached := w.cache[key]
	if cached {
		return content, nil
	}
	b, err := w.backingReader.Get(ctx, key)
	if err == nil {
		w.cache[key] = b
		return b, nil
	}
	return nil, err
}

func (w *WriteThroughStore) Put(ctx context.Context, key string, content []byte) error {
	w.cache[key] = content
	return w.backingWriter.Put(ctx, key, content)
}
