package w3rc

import (
	"context"
	"time"

	datatransferi "github.com/filecoin-project/go-data-transfer"
	datatransfer "github.com/filecoin-project/go-data-transfer/impl"
	dtnetwork "github.com/filecoin-project/go-data-transfer/network"
	gstransport "github.com/filecoin-project/go-data-transfer/transport/graphsync"
	"github.com/ipfs/go-datastore"
	gsimpl "github.com/ipfs/go-graphsync/impl"
	gsnet "github.com/ipfs/go-graphsync/network"
	"github.com/ipld/go-ipld-prime"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
)

type config struct {
	host host.Host
	ds   datastore.Batching
	dt   datatransferi.Manager

	indexerURL string
}

// An Option allows opening a Session with configured options.
type Option func(*config) error

// WithDataTransfer runs the session using an existing data transfer manager.
func WithDataTransfer(dt datatransferi.Manager) Option {
	return func(c *config) error {
		c.dt = dt
		return nil
	}
}

// WithDS sets the datastore to use for the session
func WithDS(ds datastore.Batching) Option {
	return func(c *config) error {
		c.ds = ds
		return nil
	}
}

// WithHost sets a libp2p host for the client to use.
func WithHost(h host.Host) Option {
	return func(c *config) error {
		c.host = h
		return nil
	}
}

// WithIndexer sets a URL of the indexer to use.
func WithIndexer(url string) Option {
	return func(c *config) error {
		c.indexerURL = url
		return nil
	}
}

func apply(cfg *config, opts ...Option) error {
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return err
		}
	}
	return nil
}

func applyDefaults(lsys ipld.LinkSystem, cfg *config) error {
	if cfg.host == nil {
		host, err := libp2p.New()
		if err != nil {
			return err
		}

		cfg.host = host
	}
	if cfg.ds == nil {
		cfg.ds = datastore.NewMapDatastore()
	}
	if cfg.dt == nil {
		gsNet := gsnet.NewFromLibp2pHost(cfg.host)
		gs := gsimpl.New(context.Background(), gsNet, lsys)

		dtNet := dtnetwork.NewFromLibp2pHost(cfg.host)
		tp := gstransport.NewTransport(cfg.host.ID(), gs)
		dtManager, err := datatransfer.NewDataTransfer(cfg.ds, dtNet, tp)
		if err != nil {
			log.Errorf("Failed to create data transfer subsystem: %s", err)
			return err
		}
		// Tell datatransfer to notify when ready.
		dtReady := make(chan error)
		dtManager.OnReady(func(e error) {
			dtReady <- e
			close(dtReady)
		})

		// Start datatransfer.  The context passed in allows Start to be canceled
		// if fsm migration takes too long.  Timeout for dtManager.Start() is not
		// handled here, so pass context.Background().
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err = dtManager.Start(ctx); err != nil {
			log.Errorf("Failed to start datatransfer: %s", err)
			return err
		}

		// Wait for datatransfer to be ready.
		err = <-dtReady
		if err != nil {
			return err
		}
		cfg.dt = dtManager
	}
	return nil
}
