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
//
// The returned channel of RoutingRecords must be consumed until closed by the
// Caller. The same instance of a provider may block if previous calls have
// left un-drained records. The provider will close the channel once complete
// or once the context is canceled.
type Routing interface {
	FindProviders(context.Context, cid.Cid, ...RoutingOptions) <-chan RoutingRecord
}

// A RoutingRecord is an abstract record of a content routing response.
type RoutingRecord interface {
	Request() cid.Cid
	Protocol() multicodec.Code
	Provider() interface{}
	Payload() interface{}
}

// RoutingOptions further specify a content routing request
type RoutingOptions func()

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

// Provider is empty
func (r *RoutingError) Provider() interface{} {
	return nil
}

// RecordError returns a RoutingRecord indicating a specified error
func RecordError(c cid.Cid, err error) RoutingRecord {
	return &RoutingError{c, err}
}
