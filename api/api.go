package api

import (
	"context"
	"sync"

	"github.com/ipfs-shipyard/w3rc"
	w3rcfetcher "github.com/ipfs-shipyard/w3rc/fetcher"
	"github.com/ipfs-shipyard/w3rc/gateway"
	"github.com/ipfs-shipyard/w3rc/store"
	"github.com/ipfs/go-fetcher"
	"github.com/ipfs/go-unixfsnode"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/linking"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/storage"
)

// implementation of the gateway api using w3rc

type api struct {
	opts       []w3rc.Option
	baseReader storage.ReadableStorage
	baseWriter storage.WritableStorage

	sessionLk sync.Mutex
	sessions  map[*ipld.LinkSystem]context.Context
}

func NewAPI(cacheSizeBytes uint64, opts ...w3rc.Option) gateway.API {
	cacheBase := store.NewCachingStore(cacheSizeBytes)
	a := api{
		opts:       opts,
		baseReader: cacheBase,
		baseWriter: cacheBase,
		sessions:   make(map[*ipld.LinkSystem]context.Context),
	}
	return &a
}

func (a *api) FetcherForSession(ls *ipld.LinkSystem) fetcher.Fetcher {
	fc := w3rcfetcher.FetcherConfig{
		LinkSystem:       ls,
		PrototypeChooser: w3rcfetcher.DefaultPrototypeChooser,
		Options:          a.opts,
	}

	a.sessionLk.Lock()
	defer a.sessionLk.Unlock()
	ctx, ok := a.sessions[ls]
	if !ok {
		// todo: warn
		return fc.NewSession(context.TODO())
	}
	return fc.NewSession(ctx)
}

func (a *api) NewSession(ctx context.Context) *ipld.LinkSystem {
	ls := cidlink.DefaultLinkSystem()
	derivedReadStore, derivedWriteStore := store.NewWriteThroughStore(a.baseReader, a.baseWriter)
	ls.KnownReifiers = make(map[string]linking.NodeReifier)
	ls.KnownReifiers["unixfs"] = unixfsnode.Reify
	ls.SetReadStorage(derivedReadStore)
	ls.SetWriteStorage(derivedWriteStore)
	a.sessionLk.Lock()
	defer a.sessionLk.Unlock()
	a.sessions[&ls] = ctx

	go func() {
		<-ctx.Done()
		a.sessionLk.Lock()
		defer a.sessionLk.Unlock()
		delete(a.sessions, &ls)
	}()

	return &ls
}
