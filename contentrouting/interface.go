package contentrouting

import (
	"context"

	cid "github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
)

// The Routing interface is an evolution of the ContentRouting interface
// found in [libp2p](https://github.com/libp2p/go-libp2p-core/blob/master/routing/routing.go#L26).
//
// Routing focuses only on the retrieval half of the interface: how do
// I located content given a content identifier?
type Routing interface {
	FindProvidersAsync(context.Context, cid.Cid) <-chan RoutingRecord
}

// A RoutingRecord is an abstract record of a content routing response.
type RoutingRecord interface {
	Request() cid.Cid
	Protocol() multicodec.Code
	Payload() interface{}
}

// RoutingErrorProtocol is the protocol identity for conveying a routing error
const RoutingErrorProtocol = multicodec.ReservedEnd

// RoutingError is a RoutingRecord used for signalling an underlying error
type RoutingError struct {
	cid.Cid
	Error error
}

// Request is the Cid that triggered this routing error
func (r *RoutingError) Request() cid.Cid {
	return r.Cid
}

// Protocol indicates that this record is an error
func (r *RoutingError) Protocol() multicodec.Code {
	return RoutingErrorProtocol
}

// Payload is the underlying error
func (r *RoutingError) Payload() interface{} {
	return r.Error
}

// RecordError returns a RoutingRecord indicating a specified error
func RecordError(c cid.Cid, err error) RoutingRecord {
	return &RoutingError{c, err}
}
