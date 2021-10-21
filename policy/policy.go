package policy

import (
	"context"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	"github.com/multiformats/go-multicodec"
)

// TransportRequest describes a single request over a single transport
type TransportRequest struct {
	UUID            string
	Codec           multicodec.Code
	Root            ipld.Link
	Selector        ipld.Node
	RoutingProvider interface{}
	RoutingPayload  interface{}
}

// PolicyRecord indicates one or more TransportRequests we want to execute
type PolicyRecord struct {
	TransportRequests []TransportRequest
}

// PolicyParameters indicate the kinds of parameters we want to evaluate potential providers by
type PolicyParameters interface {
	IsFree() bool
}

type RoutingRecordInterpreter interface {
	Codec() multicodec.Code
	Interpret(contentrouting.RoutingRecord) (PolicyParameters, error)
}

// The Policy interface translates routing results to exchange requests
// Essentially, it is the planning interface
type Policy interface {
	RegisterRecordInterpreter(RoutingRecordInterpreter)
	CreateRequests(ctx context.Context, root cid.Cid, selector ipld.Node, routingResults <-chan contentrouting.RoutingRecord) <-chan PolicyRecord
}
