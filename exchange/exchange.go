package exchange

import (
	"context"

	"github.com/ipld/go-ipld-prime"
	"github.com/multiformats/go-multicodec"
)

type Event interface{}

type State interface{}

type EventData struct {
	Event Event
	State State
}

type Exchange interface {
	// The identifier for this exchange protocol
	Code() multicodec.Code

	// Request data based on root, selector, and routing parameters
	RequestData(ctx context.Context, request ipld.Link, selector ipld.Node, routingProvider interface{}, routingPayload interface{}) (<-chan EventData, <-chan error)
}
