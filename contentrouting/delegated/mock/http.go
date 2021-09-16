package mockdelegatedrouter

import (
	"encoding/json"
	"net/http"
	"path"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-varint"
)

// A MockDelegatedProvider is the state of the http server acting as a counterparty
// to the HTTP delegated client.
type MockDelegatedProvider struct {
	http.Server
	provider string
	records  map[cid.Cid]MockProviderRecord
}

// A MockProviderRecord contains the response for a given CID
type MockProviderRecord struct {
	Protocol uint64
	Metadata []byte
	Provider string
}

// OnReq handles requests from the http delegated routing client
func (m *MockDelegatedProvider) OnReq(resp http.ResponseWriter, req *http.Request) {
	_, queryCid := path.Split(req.URL.Path)
	qcid, err := cid.Parse(queryCid)
	if err != nil {
		resp.WriteHeader(http.StatusNotAcceptable)
		return
	}
	if qresp, ok := m.records[qcid]; ok {
		// write response.
		rcrd := append(varint.ToUvarint(qresp.Protocol), qresp.Metadata...)
		type respStrct struct {
			CidResults []struct {
				Cid    cid.Cid
				Values []struct {
					ProviderID string
					Metadata   []byte
				}
			}
			Providers []struct {
				ID    string
				Addrs []string
			}
		}
		outVal := respStrct{
			CidResults: []struct {
				Cid    cid.Cid
				Values []struct {
					ProviderID string
					Metadata   []byte
				}
			}{{
				Cid: qcid,
				Values: []struct {
					ProviderID string
					Metadata   []byte
				}{{
					ProviderID: m.provider,
					Metadata:   rcrd,
				},
				},
			}},
			Providers: []struct {
				ID    string
				Addrs []string
			}{
				{
					ID:    m.provider,
					Addrs: []string{qresp.Provider},
				},
			},
		}
		outBytes, err := json.Marshal(outVal)
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
	mdp.provider = "mockprovider"
	mux := http.NewServeMux()
	mux.HandleFunc("/", mdp.OnReq)
	mdp.Server.Handler = mux
	mdp.records = make(map[cid.Cid]MockProviderRecord)
	return &mdp
}

// Add adds a record for the provider to return.
func (m *MockDelegatedProvider) Add(c cid.Cid, providerAddr string, protocol uint64, metadata []byte) error {
	m.records[c] = MockProviderRecord{
		Protocol: protocol,
		Metadata: metadata,
		Provider: providerAddr,
	}
	return nil
}
