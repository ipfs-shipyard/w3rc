package mockdelegatedrouter

import (
	"encoding/json"
	"net/http"
	"path"

	"github.com/filecoin-project/index-provider/metadata"
	"github.com/filecoin-project/storetheindex/api/v0/finder/model"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/multiformats/go-multihash"
)

// A MockDelegatedProvider is the state of the http server acting as a counterparty
// to the HTTP delegated client.
type MockDelegatedProvider struct {
	http.Server
	records map[string]model.MultihashResult
}

// OnReq handles requests from the http delegated routing client
func (m *MockDelegatedProvider) OnReq(resp http.ResponseWriter, req *http.Request) {
	_, queryCid := path.Split(req.URL.Path)
	mh, err := multihash.FromB58String(queryCid)
	if err != nil {
		resp.WriteHeader(http.StatusNotAcceptable)
		return
	}
	if qresp, ok := m.records[string(mh)]; ok {
		outBytes, err := json.Marshal(model.FindResponse{
			MultihashResults: []model.MultihashResult{qresp},
		})
		if err != nil {
			resp.WriteHeader(http.StatusInternalServerError)
			return
		}

		resp.Write(outBytes)
	} else {
		resp.WriteHeader(http.StatusNotFound)
		return
	}
}

// New creates a mock provider that can serve as a counterpart to the http
// delegated content routing client
func New() *MockDelegatedProvider {
	mdp := MockDelegatedProvider{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", mdp.OnReq)
	mdp.Server.Handler = mux
	mdp.records = make(map[string]model.MultihashResult)
	return &mdp
}

// Add adds a record for the provider to return.
func (m *MockDelegatedProvider) Add(c cid.Cid, peerAddr peer.AddrInfo, md metadata.Protocol) error {
	mdb := metadata.New(md)
	mdbb, err := mdb.MarshalBinary()
	if err != nil {
		return err
	}
	m.records[string(c.Hash())] = model.MultihashResult{
		Multihash: c.Hash(),
		ProviderResults: []model.ProviderResult{
			{
				ContextID: []byte("something"),
				Metadata:  mdbb,
				Provider:  peerAddr,
			},
		},
	}
	return nil
}
