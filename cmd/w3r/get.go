package main

import (
	"fmt"

	"github.com/ipfs-shipyard/w3rc"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-car/v2/blockstore"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/storage/bsadapter"
	"github.com/ipld/go-ipld-prime/storage/memstore"
	"github.com/ipld/go-ipld-prime/traversal/selector"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"github.com/urfave/cli/v2"
)

// Get retrieves an individual CID or dag
func Get(c *cli.Context) error {
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
	s, err := selector.CompileSelector(selectorSpec)
	if err != nil {
		return err
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
	} else {
		ls.SetReadStorage(&store)
		ls.SetWriteStorage(&store)
	}

	w3s := w3rc.NewSession(ls)
	if w3s == nil {
		return fmt.Errorf("failed to create session")
	}
	if _, err = w3s.Get(c.Context, parsedCid, s); err != nil {
		return err
	}

	// print
	if !c.IsSet("file") {
		outStream := c.App.Writer
		bytes, err := store.Get(c.Context, parsedCid.KeyString())
		if err != nil {
			return err
		}
		_, _ = outStream.Write(bytes)
		//todo: handle recursive to stdout by writing the car
	}

	return nil
}
