package w3rc

import (
	"context"
	"crypto/rand"

	datatransferi "github.com/filecoin-project/go-data-transfer"
	datatransfer "github.com/filecoin-project/go-data-transfer/impl"
	dtnetwork "github.com/filecoin-project/go-data-transfer/network"
	gstransport "github.com/filecoin-project/go-data-transfer/transport/graphsync"
	"github.com/ipfs/go-datastore"
	gsimpl "github.com/ipfs/go-graphsync/impl"
	gsnet "github.com/ipfs/go-graphsync/network"
	"github.com/ipld/go-ipld-prime"
	csms "github.com/libp2p/go-conn-security-multistream"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/transport"
	mplex "github.com/libp2p/go-libp2p-mplex"
	noise "github.com/libp2p/go-libp2p-noise"
	"github.com/libp2p/go-libp2p-peerstore/pstoremem"
	swarm "github.com/libp2p/go-libp2p-swarm"
	tls "github.com/libp2p/go-libp2p-tls"
	tptu "github.com/libp2p/go-libp2p-transport-upgrader"
	yamux "github.com/libp2p/go-libp2p-yamux"
	basichost "github.com/libp2p/go-libp2p/p2p/host/basic"
	msmux "github.com/libp2p/go-stream-muxer-multistream"
	"github.com/libp2p/go-tcp-transport"
	ws "github.com/libp2p/go-ws-transport"
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
		priv, pub, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
		if err != nil {
			return err
		}

		pid, err := peer.IDFromPublicKey(pub)
		if err != nil {
			return err
		}

		ps, err := pstoremem.NewPeerstore()
		if err != nil {
			return err
		}

		if err := ps.AddPrivKey(pid, priv); err != nil {
			return err
		}
		if err := ps.AddPubKey(pid, pub); err != nil {
			return err
		}

		net, err := swarm.NewSwarm(pid, ps)
		if err != nil {
			return err
		}

		host, err := basichost.NewHost(net, &basichost.HostOpts{})
		if err != nil {
			return err
		}

		secMuxer := new(csms.SSMuxer)
		noiseSec, _ := noise.New(priv)
		secMuxer.AddTransport(noise.ID, noiseSec)
		tlsSec, _ := tls.New(priv)
		secMuxer.AddTransport(tls.ID, tlsSec)

		muxMuxer := msmux.NewBlankTransport()
		muxMuxer.AddTransport("/yamux/1.0.0", yamux.DefaultTransport)
		muxMuxer.AddTransport("/mplex/6.7.0", mplex.DefaultTransport)
		upgrader, err := tptu.New(secMuxer, muxMuxer)
		if err != nil {
			return err
		}

		tcpT, _ := tcp.NewTCPTransport(upgrader, net.ResourceManager())
		for _, t := range []transport.Transport{
			tcpT,
			ws.New(upgrader, net.ResourceManager()),
		} {
			if err := net.AddTransport(t); err != nil {
				return err
			}
		}

		host.Start()
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
		})

		// Start datatransfer.  The context passed in allows Start to be canceled
		// if fsm migration takes too long.  Timeout for dtManager.Start() is not
		// handled here, so pass context.Background().
		if err = dtManager.Start(context.Background()); err != nil {
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
