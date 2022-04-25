package planning

import (
	"github.com/ipld/go-ipld-prime"
	"github.com/multiformats/go-multicodec"
)

// TransportRequest describes a single request over a single transport
type TransportRequest struct {
	Codec           multicodec.Code
	Root            ipld.Link
	Selector        ipld.Node
	RoutingProvider interface{}
	RoutingPayload  interface{}
}

// TransportPlan indicates one or more TransportRequests we want to execute
type TransportPlan struct {
	TransportRequests []*TransportRequest
	Error             error
}
