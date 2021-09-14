package delegated_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs-shipyard/w3rc/contentrouting/delegated"
	mockdelegatedrouter "github.com/ipfs-shipyard/w3rc/contentrouting/delegated/mock"
	"github.com/ipfs/go-cid"
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

	cr := delegated.NewDelegatedHTTP(fmt.Sprintf("http://%s/", listener.Addr().String()))

	// A valid record:
	foundMH, _ := multihash.Encode([]byte("hello world"), multihash.IDENTITY)
	foundCid := cid.NewCidV1(uint64(multicodec.DagPb), foundMH)
	serv.Add(foundCid, "/ip/127.0.0.1/tcp/8080/tls", 1, []byte("hello data"))
	rcrdChan := cr.FindProviders(context.Background(), foundCid)
	rcrds := doDrain(rcrdChan)
	if len(rcrds) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rcrds))
	}
	if rcrds[0].Protocol() != 1 {
		t.Fatalf("expected protocol '1', got %d", rcrds[0].Protocol())
	}
	if !bytes.Equal(rcrds[0].Payload().([]byte), []byte("hello data")) {
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
	serv.Add(otherCid, "/ip/127.0.0.1/tcp/8080/tls", contentrouting.RoutingErrorProtocol, []byte("error"))
	rcrdChan = cr.FindProviders(context.Background(), otherCid)
	rcrds = doDrain(rcrdChan)
	if len(rcrds) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rcrds))
	}
	if rcrds[0].Protocol() != contentrouting.RoutingErrorProtocol {
		t.Fatalf("expected error, got %d", rcrds[0].Protocol())
	}

	return
}

func doDrain(c <-chan contentrouting.RoutingRecord) []contentrouting.RoutingRecord {
	rcrds := make([]contentrouting.RoutingRecord, 0)
	for e := range c {
		rcrds = append(rcrds, e)
	}
	return rcrds
}
