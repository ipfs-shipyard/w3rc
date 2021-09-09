package contentrouting

import (
	"context"

	"github.com/multiformats/go-multicodec"
	cid "github.com/ipfs/go-cid"
)

// The Routing interface is an evolution of the ContentRouting interface
// found in [libp2p](https://github.com/libp2p/go-libp2p-core/blob/master/routing/routing.go#L26).
//
// Routing focuses only on the retrieval half of the interface: how do
// I located content given a content identifier?
interface Routing {
	FindProvidersAsync(context.Context, cid.Cid) <-chan RoutingRecord
}


// A RoutingRecord is an abstract record of a content routing response.
interface RoutingRecord {
	Request() cid.Cid
	Protocol() multicodec.Code
	Payload() interface{}
}
