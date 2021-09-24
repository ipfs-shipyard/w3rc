package delegated

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-varint"

	cid "github.com/ipfs/go-cid"
)

// NewDelegatedHTTP makes a routing provider backed by an HTTP endpoint.
func NewDelegatedHTTP(url string) contentrouting.Routing {
	return &HTTPRouter{
		prefix: url,
	}
}

// HTTPRouter contains the state for an active delegated HTTP client.
type HTTPRouter struct {
	prefix string
	http.Transport
}

// HTTPResponseResult is the expected type of an individual query response from a server
type HTTPResponseResult struct {
	Cid    cid.Cid
	Values []IndexerValue
}

// IndexerValue is the response metadata of an individual record
type IndexerValue struct {
	ProviderID string
	Metadata   []byte
}

// Extract separates the protocol from the protocol-specific metadata in a record
func (v *IndexerValue) Extract() (uint64, []byte, error) {
	protocol, len, err := varint.FromUvarint(v.Metadata)
	if err != nil {
		return 0, nil, err
	}
	return protocol, v.Metadata[len:], nil
}

// PeerAddrInfo contains the addresses of a provider
type PeerAddrInfo struct {
	ID    string
	Addrs []string
}

// HTTPResponse is the full response expected from a delegated router
type HTTPResponse struct {
	CidResults []HTTPResponseResult
	Providers  []PeerAddrInfo
}

// FindProviders implements the content routing interface
func (hr *HTTPRouter) FindProviders(ctx context.Context, c cid.Cid, _ ...contentrouting.RoutingOptions) <-chan contentrouting.RoutingRecord {
	ch := make(chan contentrouting.RoutingRecord, 1)
	go func() {
		defer close(ch)
		cli := &http.Client{
			Transport: &hr.Transport,
		}

		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s%s", hr.prefix, c), nil)
		if err != nil {
			ch <- contentrouting.RecordError(c, err)
			return
		}
		resp, err := cli.Do(req)
		if err != nil {
			ch <- contentrouting.RecordError(c, err)
			return
		}

		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusNotFound {
				// no error, but close channel to indicate no responses.
				return
			}
			ch <- contentrouting.RecordError(c, fmt.Errorf("routing request returned status %d: %s", resp.StatusCode, resp.Status))
			return
		}

		dec := json.NewDecoder(resp.Body)
		defer resp.Body.Close()

		parsedResp := HTTPResponse{}
		if err := dec.Decode(&parsedResp); err != nil {
			ch <- contentrouting.RecordError(c, err)
			return
		}
		fmt.Printf("decode to %v\n", parsedResp)

		// turn parsedResp into records.
		for _, candidateCid := range parsedResp.CidResults {
			if !candidateCid.Cid.Equals(c) {
				continue
			}
			for _, val := range candidateCid.Values {
				code, md, err := val.Extract()
				if err != nil {
					// TODO: warn
					continue
				}
				ch <- &httpRecord{Cid: c, Proto: multicodec.Code(code), Metadata: md, ProviderID: val.ProviderID}
			}
		}
	}()

	return ch
}

type httpRecord struct {
	Cid        cid.Cid
	Proto      multicodec.Code
	Metadata   []byte
	ProviderID string
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
	return r.Metadata
}

// Payload is the underlying error
func (r *httpRecord) Provider() interface{} {
	return r.ProviderID
}
