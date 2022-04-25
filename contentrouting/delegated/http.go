package delegated

import (
	"context"

	"github.com/filecoin-project/index-provider/metadata"
	finderhttpclient "github.com/filecoin-project/storetheindex/api/v0/finder/client/http"
	"github.com/filecoin-project/storetheindex/api/v0/finder/model"
	"github.com/ipfs-shipyard/w3rc/contentrouting"
	cid "github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-varint"
)

// NewDelegatedHTTP makes a routing provider backed by an HTTP endpoint.
func NewDelegatedHTTP(url string) (contentrouting.Routing, error) {
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
				ch <- &httpRecord{Cid: c, Val: val}
			}
		}
	}()

	return ch
}

type httpRecord struct {
	Cid cid.Cid
	Val model.ProviderResult
}

// Request is the Cid that triggered this routing error
func (r *httpRecord) Request() cid.Cid {
	return r.Cid
}

// Protocol indicates that this record is an error
func (r *httpRecord) Protocol() multicodec.Code {
	code, _, err := varint.FromUvarint(r.Val.Metadata)
	if err == nil {
		return multicodec.Code(code)
	}
	return 0
}

// Payload is the underlying error
func (r *httpRecord) Payload() interface{} {
	md := metadata.Metadata{}
	if err := md.UnmarshalBinary(r.Val.Metadata); err != nil {
		return nil
	}
	fp := r.Protocol()
	return md.Get(fp)
}

// Payload is the underlying error
func (r *httpRecord) Provider() interface{} {
	return r.Val.Provider
}
