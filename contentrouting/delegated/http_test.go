package delegated_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/filecoin-project/index-provider/metadata"
	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs-shipyard/w3rc/contentrouting/delegated"
	mockdelegatedrouter "github.com/ipfs-shipyard/w3rc/contentrouting/delegated/mock"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p-core/peer"
	p2ptestutil "github.com/libp2p/go-libp2p-testing/netutil"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
)

func TestHTTPFetch(t *testing.T) {
	serv := mockdelegatedrouter.New()
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
		return
	}
	go serv.Serve(listener)
	defer serv.Close()

	cr, err := delegated.NewDelegatedHTTP(fmt.Sprintf("http://%s/", listener.Addr().String()))
	if err != nil {
		t.Fatal(err)
	}
	p, err := p2ptestutil.RandTestBogusIdentity()
	if err != nil {
		t.Fatal(err)
	}
	// A valid record:
	foundMH, _ := multihash.Encode([]byte("hello world"), multihash.IDENTITY)
	foundCid := cid.NewCidV1(uint64(multicodec.DagPb), foundMH)
	addr := peer.AddrInfo{
		ID: p.ID(),
		Addrs: []multiaddr.Multiaddr{
			multiaddr.StringCast("/ip4/127.0.0.1/tcp/8080/tls"),
		},
	}
	serv.Add(foundCid, addr, metadata.Bitswap{})
	rcrdChan := cr.FindProviders(context.Background(), foundCid)
	rcrds := doDrain(rcrdChan)
	if len(rcrds) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rcrds))
	}
	if rcrds[0].Protocol() != multicodec.TransportBitswap {
		t.Fatalf("expected bitswap protocol , got %d", rcrds[0].Protocol())
	}
	if rcrds[0].Payload() != nil {
		t.Fatal("unexpected payload")
	}

	// An unknown record:
	otherMH, _ := multihash.Encode([]byte("differentCID"), multihash.IDENTITY)
	otherCid := cid.NewCidV1(uint64(multicodec.DagPb), otherMH)
	rcrdChan = cr.FindProviders(context.Background(), otherCid)
	rcrds = doDrain(rcrdChan)
	if len(rcrds) != 0 {
		t.Fatalf("expected no record, got %d", len(rcrds))
	}

	// An invalid record:
	proto := metadata.GraphsyncFilecoinV1{PieceCID: cid.NewCidV1(0, []byte{0}), VerifiedDeal: true, FastRetrieval: true}
	serv.Add(otherCid, addr, &proto)
	rcrdChan = cr.FindProviders(context.Background(), otherCid)
	rcrds = doDrain(rcrdChan)
	if len(rcrds) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rcrds))
	}
	if rcrds[0].Protocol() != multicodec.TransportGraphsyncFilecoinv1 {
		t.Fatalf("expected error, got %d", rcrds[0].Protocol())
	}
}

func doDrain(c <-chan contentrouting.RoutingRecord) []contentrouting.RoutingRecord {
	rcrds := make([]contentrouting.RoutingRecord, 0)
	for e := range c {
		rcrds = append(rcrds, e)
	}
	return rcrds
}
