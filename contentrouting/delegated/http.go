package delegated

import (
	"context"
	"fmt"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	cid "github.com/ipfs/go-cid"
	finderhttpclient "github.com/ipni/go-libipni/find/client/http"
	metadata "github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multicodec"
)

// NewDelegatedHTTP makes a routing provider backed by an HTTP endpoint.
func NewDelegatedHTTP(url string) (contentrouting.Routing, error) {
	fmt.Printf("delegated to %s\n", url)
	client, err := finderhttpclient.New(url)
	if err != nil {
		return nil, err
	}
	return &HTTPRouter{
		Client: client,
	}, nil
}

// HTTPRouter contains the state for an active delegated HTTP client.
type HTTPRouter struct {
	*finderhttpclient.Client
}

// FindProviders implements the content routing interface
func (hr *HTTPRouter) FindProviders(ctx context.Context, c cid.Cid, _ ...contentrouting.RoutingOptions) <-chan contentrouting.RoutingRecord {
	ch := make(chan contentrouting.RoutingRecord, 1)
	go func() {
		defer close(ch)

		parsedResp, err := hr.Client.Find(ctx, c.Hash())
		if err != nil {
			ch <- contentrouting.RecordError(c, err)
			return
		}

		hash := string(c.Hash())
		// turn parsedResp into records.
		for _, multihashResult := range parsedResp.MultihashResults {
			if !(string(multihashResult.Multihash) == hash) {
				continue
			}
			for _, val := range multihashResult.ProviderResults {
				var md metadata.Metadata
				if err := md.UnmarshalBinary(val.Metadata); err != nil {
					ch <- &httpRecord{Cid: c, Prov: val.Provider, Proto: multicodec.Identity, Value: val.Metadata}
					continue
				}
				for _, p := range md.Protocols() {
					ch <- &httpRecord{Cid: c, Prov: val.Provider, Proto: p, Value: md.Get(p)}
				}
			}
		}
	}()

	return ch
}

type httpRecord struct {
	Cid   cid.Cid
	Prov  peer.AddrInfo
	Proto multicodec.Code
	Value interface{}
}

// Request is the Cid that triggered this routing error
func (r *httpRecord) Request() cid.Cid {
	return r.Cid
}

// Protocol indicates that this record is an error
func (r *httpRecord) Protocol() multicodec.Code {
	return r.Proto
}

// Payload is the underlying error
func (r *httpRecord) Payload() interface{} {
	return r.Value
}

// Payload is the underlying error
func (r *httpRecord) Provider() interface{} {
	return r.Prov
}
