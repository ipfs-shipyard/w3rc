package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	hma "github.com/filecoin-project/go-legs/httpsync/multiaddr"
	"github.com/ipfs-shipyard/w3rc/exchange"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipld/go-ipld-prime/linking"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/traversal"
	ipldselector "github.com/ipld/go-ipld-prime/traversal/selector"
	peer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multicodec"
)

func NewHTTPExchange(client *http.Client, lsys *ipld.LinkSystem) *HTTPExchange {
	return &HTTPExchange{
		client: client,
		lsys:   lsys,
	}
}

type HTTPExchange struct {
	client *http.Client
	lsys   *ipld.LinkSystem
}

func (*HTTPExchange) Code() multicodec.Code {
	return multicodec.Code(multicodec.Http)
}

func singleTerminalError(err error) <-chan exchange.EventData {
	resultChan := make(chan exchange.EventData)
	resultChan <- exchange.EventData{Event: exchange.FailureEvent, State: err}
	close(resultChan)
	return resultChan
}

func toURL(ai peer.AddrInfo) (string, error) {
	url := ""
	for _, ma := range ai.Addrs {
		url, err := hma.ToURL(ma)
		if err == nil {
			return url.String(), nil
		}
	}
	return url, fmt.Errorf("no http multiaddr found")
}

func (he *HTTPExchange) RequestData(ctx context.Context, root ipld.Link, selector ipld.Node, routingProvider interface{}, routingPayload interface{}) <-chan exchange.EventData {
	ai, ok := routingProvider.(peer.AddrInfo)
	if !ok {
		return singleTerminalError(fmt.Errorf("routing provider is not in expected format"))
	}
	base, err := toURL(ai)
	if err != nil {
		return singleTerminalError(err)
	}

	respChan := make(chan exchange.EventData)
	sel, err := ipldselector.CompileSelector(selector)
	if err != nil {
		return singleTerminalError(fmt.Errorf("failed to compile selector: %q", err))
	}
	go he.traverse(ctx, root, sel, base, respChan)

	return respChan
}

func (he *HTTPExchange) traverse(ctx context.Context, root ipld.Link, s ipldselector.Selector, base string, status chan exchange.EventData) {
	ls := cidlink.DefaultLinkSystem()

	ls.StorageReadOpener = func(lc linking.LinkContext, l datamodel.Link) (io.Reader, error) {
		r, err := he.lsys.StorageReadOpener(lc, l)
		if err == nil {
			return r, nil
		}

		w, writeCommitter, err := he.lsys.StorageWriteOpener(lc)
		if err != nil {
			return nil, err
		}

		resp, err := he.client.Get(base + l.(cidlink.Link).Cid.String())
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("request not successful for %s: %d", l.(cidlink.Link).Cid, resp.StatusCode)
		}
		var buf bytes.Buffer
		if _, err := io.Copy(w, io.TeeReader(resp.Body, &buf)); err != nil {
			return nil, err
		}
		if err := writeCommitter(l); err != nil {
			return nil, err
		}
		return &buf, nil
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

func (he *HTTPExchange) Close() {
}
