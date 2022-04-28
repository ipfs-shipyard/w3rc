package fetcher

import (
	"context"

	"github.com/ipfs-shipyard/w3rc"
	"github.com/ipfs/go-fetcher"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/schema"
	"github.com/ipld/go-ipld-prime/traversal"
	"github.com/ipld/go-ipld-prime/traversal/selector"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
)

//implementation of github.com/ipfs/go-fetcher using a w3rc

// FetcherConfig defines a configuration object from which Fetcher instances are constructed
type FetcherConfig struct {
	*ipld.LinkSystem
	Options          []w3rc.Option
	PrototypeChooser traversal.LinkTargetNodePrototypeChooser
}

// NewFetcherConfig creates a FetchConfig from which session may be created and nodes retrieved.
func NewFetcherConfig() FetcherConfig {
	ls := cidlink.DefaultLinkSystem()
	return FetcherConfig{
		LinkSystem:       &ls,
		PrototypeChooser: DefaultPrototypeChooser,
	}
}

func (fc FetcherConfig) NewSession(ctx context.Context) fetcher.Fetcher {
	session, err := w3rc.NewSession(*fc.LinkSystem, fc.Options...)
	if err != nil {
		return nil
	}
	return fc.FetcherWithSession(ctx, session)
}

func (fc FetcherConfig) FetcherWithSession(ctx context.Context, session w3rc.Session) fetcher.Fetcher {
	protoChooser := fc.PrototypeChooser
	return &fetcherSession{LinkSystem: fc.LinkSystem, Session: session, protoChooser: protoChooser}
}

type fetcherSession struct {
	*ipld.LinkSystem
	w3rc.Session
	protoChooser traversal.LinkTargetNodePrototypeChooser
}

func (f *fetcherSession) getOrLoad(ctx context.Context, link ipld.Link, ptype ipld.NodePrototype) (ipld.Node, error) {
	has, err := f.LinkSystem.Load(ipld.LinkContext{}, link, ptype)
	if err != nil {
		rc := link.(cidlink.Link).Cid
		return f.Session.Get(ctx, rc, selectorparse.CommonSelector_MatchPoint)
	}
	return has, nil
}

func (f *fetcherSession) BlockOfType(ctx context.Context, link ipld.Link, ptype ipld.NodePrototype) (ipld.Node, error) {
	return f.getOrLoad(ctx, link, ptype)
}

func (f *fetcherSession) nodeMatching(ctx context.Context, initialProgress traversal.Progress, node ipld.Node, match ipld.Node, cb fetcher.FetchCallback) error {
	matchSelector, err := selector.ParseSelector(match)
	if err != nil {
		return err
	}
	return initialProgress.WalkMatching(node, matchSelector, func(prog traversal.Progress, n ipld.Node) error {
		return cb(fetcher.FetchResult{
			Node:          n,
			Path:          prog.Path,
			LastBlockPath: prog.LastBlock.Path,
			LastBlockLink: prog.LastBlock.Link,
		})
	})
}

func (f *fetcherSession) blankProgress(ctx context.Context) traversal.Progress {
	return traversal.Progress{
		Cfg: &traversal.Config{
			Ctx:                            ctx,
			LinkSystem:                     *f.LinkSystem,
			LinkTargetNodePrototypeChooser: f.protoChooser,
		},
	}
}

func (f *fetcherSession) NodeMatching(ctx context.Context, node ipld.Node, match ipld.Node, cb fetcher.FetchCallback) error {
	return f.nodeMatching(ctx, f.blankProgress(ctx), node, match, cb)
}

func (f *fetcherSession) BlockMatchingOfType(ctx context.Context, root ipld.Link, match ipld.Node,
	_ ipld.NodePrototype, cb fetcher.FetchCallback) error {

	// retrieve first node
	prototype, err := f.PrototypeFromLink(root)
	if err != nil {
		return err
	}
	node, err := f.BlockOfType(ctx, root, prototype)
	if err != nil {
		return err
	}

	progress := f.blankProgress(ctx)
	progress.LastBlock.Link = root
	return f.nodeMatching(ctx, progress, node, match, cb)
}

func (f *fetcherSession) PrototypeFromLink(lnk ipld.Link) (ipld.NodePrototype, error) {
	return f.protoChooser(lnk, ipld.LinkContext{})
}

// DefaultPrototypeChooser supports choosing the prototype from the link and falling
// back to a basicnode.Any builder
var DefaultPrototypeChooser = func(lnk ipld.Link, lnkCtx ipld.LinkContext) (ipld.NodePrototype, error) {
	if tlnkNd, ok := lnkCtx.LinkNode.(schema.TypedLinkNode); ok {
		return tlnkNd.LinkTargetNodePrototype(), nil
	}
	return basicnode.Prototype.Any, nil
}
