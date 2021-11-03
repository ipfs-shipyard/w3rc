package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	mockdelegatedrouter "github.com/ipfs-shipyard/w3rc/contentrouting/delegated/mock"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p-core/peer"
	p2ptestutil "github.com/libp2p/go-libp2p-netutil"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"w3rc": run,
	}))
}

func TestScript(t *testing.T) {

	// Set up mock discoverable CID
	p, err := p2ptestutil.RandTestBogusIdentity()
	if err != nil {
		t.Fatal(err)
	}
	mh, _ := multihash.Encode([]byte("fish"), multihash.IDENTITY)
	foundCid := cid.NewCidV1(uint64(multicodec.DagPb), mh)
	addr := peer.AddrInfo{
		ID: p.ID(),
		Addrs: []multiaddr.Multiaddr{
			multiaddr.StringCast("/ip4/127.0.0.1/tcp/1234/tls"),
		},
	}

	// Start mock indexer and make the mock CID discoverable
	mockIndexer := mockdelegatedrouter.New()
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
		return
	}
	go func() {
		err := mockIndexer.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("failed to start mock indexer server: %v", err)
		}
	}()
	t.Cleanup(func() {
		if err := mockIndexer.Close(); err != nil {
			t.Fatalf("failed to close mock indexer: %v", err)
		}
	})
	if err = mockIndexer.Add(foundCid, addr, 1, []byte("hello data")); err != nil {
		t.Fatalf("failed to add mock discoverable CID: %v", err)
	}

	t.Parallel()
	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("testdata", "script"),
		Setup: func(env *testscript.Env) error {
			mockIndexerAddr := fmt.Sprintf("http://%s", listener.Addr().String())
			env.Setenv("MOCK_INDEXER", mockIndexerAddr)
			env.Setenv("MOCK_DISCOVERABLE_CID", foundCid.String())
			return nil
		},
	})
}
