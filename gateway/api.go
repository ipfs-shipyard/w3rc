package gateway

import (
	"context"

	"github.com/ipfs/go-fetcher"
	"github.com/ipld/go-ipld-prime"
)

type API interface {
	ResolvePath(context.Context, Path) (Path, error)

	FetcherForSession(*ipld.LinkSystem) fetcher.Fetcher
	NewSession(context.Context) *ipld.LinkSystem
}
