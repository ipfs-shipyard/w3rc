package main

import (
	"fmt"

	"github.com/ipfs-shipyard/w3rc"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-log/v2"
	"github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/storage/bsadapter"
	"github.com/ipld/go-ipld-prime/storage/memstore"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"github.com/urfave/cli/v2"
)

// Get retrieves an individual CID or dag
func Get(c *cli.Context) error {
	if c.Bool("verbose") {
		log.SetLogLevel("*", "debug")
	}
	var err error
	store := memstore.Store{}

	if c.NArg() < 1 {
		return fmt.Errorf("must provide a CID to fetch")
	}

	parsedCid, err := cid.Decode(c.Args().First())
	if err != nil {
		return err
	}

	selectorSpec := selectorparse.CommonSelector_MatchPoint
	if c.Bool("recursive") {
		selectorSpec = selectorparse.CommonSelector_MatchAllRecursively
	}

	ls := cidlink.DefaultLinkSystem()
	if c.IsSet("file") {
		bs, err := blockstore.OpenReadWrite(c.String("file"), []cid.Cid{parsedCid})
		if err != nil {
			return err
		}
		bsa := bsadapter.Adapter{Wrapped: bs}
		ls.SetReadStorage(&bsa)
		ls.SetWriteStorage(&bsa)
		defer bs.Finalize()
	} else {
		ls.SetReadStorage(&store)
		ls.SetWriteStorage(&store)
	}

	opts := []w3rc.Option{}
	if c.IsSet("indexer") {
		opts = append(opts, w3rc.WithIndexer(c.String("indexer")))
	}
	w3s, err := w3rc.NewSession(ls, opts...)
	if err != nil {
		return err
	}
	if w3s == nil {
		return fmt.Errorf("failed to create session")
	}
	if _, err = w3s.Get(c.Context, parsedCid, selectorSpec); err != nil {
		return err
	}

	// print
	if !c.IsSet("file") {
		outStream := c.App.Writer
		_, err := car.TraverseV1(c.Context, &ls, parsedCid, selectorSpec, outStream)
		if err != nil {
			return err
		}
	}

	return nil
}
