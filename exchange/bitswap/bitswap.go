package bitswap

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/ipfs-shipyard/w3rc/exchange"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipld/go-ipld-prime/linking"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/traversal"
	ipldselector "github.com/ipld/go-ipld-prime/traversal/selector"
	"github.com/libp2p/go-libp2p/core/host"
	peer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/multiformats/go-multicodec"
	bsc "github.com/willscott/go-selfish-bitswap-client"
)

func NewBitswapExchange(h host.Host, lsys *ipld.LinkSystem) *BitswapExchange {
	return &BitswapExchange{
		h:        h,
		lsys:     lsys,
		sessions: make(map[peer.ID]*bsc.Session),
	}
}

type BitswapExchange struct {
	h        host.Host
	lsys     *ipld.LinkSystem
	sessions map[peer.ID]*bsc.Session
}

func (*BitswapExchange) Code() multicodec.Code {
	return multicodec.TransportBitswap
}

func singleTerminalError(err error) <-chan exchange.EventData {
	resultChan := make(chan exchange.EventData)
	resultChan <- exchange.EventData{Event: exchange.FailureEvent, State: err}
	close(resultChan)
	return resultChan
}

func (be *BitswapExchange) RequestData(ctx context.Context, root ipld.Link, selector ipld.Node, routingProvider interface{}, routingPayload interface{}) <-chan exchange.EventData {
	ai, ok := routingProvider.(peer.AddrInfo)
	if !ok {
		return singleTerminalError(fmt.Errorf("routing provider is not in expected format"))
	}

	if _, ok := be.sessions[ai.ID]; !ok {
		be.h.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.TempAddrTTL)
		be.sessions[ai.ID] = bsc.New(be.h, ai.ID)
	}

	sess := be.sessions[ai.ID]

	respChan := make(chan exchange.EventData)
	sel, err := ipldselector.CompileSelector(selector)
	if err != nil {
		return singleTerminalError(fmt.Errorf("failed to compile selector: %q", err))
	}
	go be.traverse(ctx, root, sel, sess, respChan)

	return respChan
}

func (be *BitswapExchange) traverse(ctx context.Context, root ipld.Link, s ipldselector.Selector, session *bsc.Session, status chan exchange.EventData) {
	ls := cidlink.DefaultLinkSystem()

	ls.StorageReadOpener = func(lc linking.LinkContext, l datamodel.Link) (io.Reader, error) {
		r, err := be.lsys.StorageReadOpener(lc, l)
		if err == nil {
			return r, nil
		}

		w, writeCommitter, err := be.lsys.StorageWriteOpener(lc)
		if err != nil {
			return nil, err
		}

		data, err := session.Get(l.(cidlink.Link).Cid)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
		if err := writeCommitter(l); err != nil {
			return nil, err
		}
		return bytes.NewReader(data), nil
	}

	prog := traversal.Progress{
		Cfg: &traversal.Config{
			Ctx:               ctx,
			LinkSystem:        ls,
			LinkVisitOnlyOnce: true,
		},
	}
	status <- exchange.EventData{Event: exchange.StartEvent, State: nil}

	err := prog.WalkAdv(basicnode.NewLink(root), s, func(prog traversal.Progress, _ ipld.Node, _ traversal.VisitReason) error {
		status <- exchange.EventData{Event: exchange.ProgressEvent, State: prog.LastBlock.Link}
		return nil
	})
	if err != nil {
		status <- exchange.EventData{Event: exchange.FailureEvent, State: err}
	} else {
		status <- exchange.EventData{Event: exchange.SuccessEvent, State: root}
	}
}

func (be *BitswapExchange) Close() {
	for _, s := range be.sessions {
		_ = s.Close()
	}
}
