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

type HTTPRouter struct {
	prefix string
	http.Transport
}

type HTTPResponseResult struct {
	Cid    cid.Cid
	Values []IndexerValue
}

type IndexerValue struct {
	ProviderID string
	Metadata   []byte
}

func (v *IndexerValue) Extract() (uint64, []byte, error) {
	protocol, len, err := varint.FromUvarint(v.Metadata)
	if err != nil {
		return 0, nil, err
	}
	return protocol, v.Metadata[len:], nil
}

type PeerAddrInfo struct {
	ID    string
	Addrs []string
}

type HTTPResponse struct {
	CidResults []HTTPResponseResult
	Providers  []PeerAddrInfo
}

func (hr *HTTPRouter) FindProvidersAsync(ctx context.Context, c cid.Cid) <-chan contentrouting.RoutingRecord {
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
				ch <- &httpRecord{Cid: c, Proto: multicodec.Code(code), Metadata: md}
			}
		}
	}()

	return ch
}

type httpRecord struct {
	Cid      cid.Cid
	Proto    multicodec.Code
	Metadata []byte
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
