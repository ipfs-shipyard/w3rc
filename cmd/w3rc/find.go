package main

import (
	"context"
	"fmt"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs-shipyard/w3rc/contentrouting/delegated"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/urfave/cli/v2"
)

var findCommand = &cli.Command{
	Name:      "find",
	Usage:     "Finds a list of providers for a given CID",
	Action:    findAction,
	Flags:     []cli.Flag{indexerFlag, timeoutFlag},
	ArgsUsage: "<cid>",
}

func findAction(cctx *cli.Context) error {
	if !cctx.Args().Present() {
		return fmt.Errorf("a CID for which to find providers must be specified")
	}

	routing, err := delegated.NewDelegatedHTTP(indexer)
	if err != nil {
		return err
	}

	cidArg := cctx.Args().First()
	target, err := cid.Decode(cidArg)
	if err != nil {
		return fmt.Errorf("failed to parse CID: %w", err)
	}

	ctxWithTimeout, cancel := context.WithTimeout(cctx.Context, timeout)
	defer cancel()
	providersChan := routing.FindProviders(ctxWithTimeout, target)
	var providers []peer.AddrInfo
	for rr := range providersChan {
		if rErr, ok := rr.(*contentrouting.RoutingError); ok {
			return rErr.Error
		}

		p, ok := rr.Provider().(peer.AddrInfo)
		if !ok {
			return fmt.Errorf("expected provider peer.AddrInfo in routing record but found: %v", rr.Provider())
		}
		providers = append(providers, p)
	}
	_, err = fmt.Fprintf(cctx.App.Writer, "Found %d provider(s) for CID %s\n", len(providers), cidArg)
	if err != nil {
		return err
	}

	for _, p := range providers {
		_, err = fmt.Fprintf(cctx.App.Writer, "\t%v:\t%v\n", p.ID, p.Addrs)
		if err != nil {
			return err
		}
	}
	return nil
}
