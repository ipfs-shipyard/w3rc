package planning

import (
	"context"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
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
	TransportRequests []TransportRequest
}

func NewSimpleTransportPlan(targetRoot cid.Cid, targetSelector ipld.Node, rr contentrouting.RoutingRecord) TransportPlan {
	return TransportPlan{
		TransportRequests: []TransportRequest{
			{
				Codec:           rr.Protocol(),
				Root:            cidlink.Link{Cid: targetRoot},
				Selector:        targetSelector,
				RoutingProvider: rr.Provider(),
				RoutingPayload:  rr.Payload(),
			},
		},
	}
}

// SinglePlanner takes a stream of possible transport requests we can make
// and generates a single transport plan from that
// And implementation might consider:
// - minimum policy score
// - minimum number of results to receive before executing
// - how many requests to make at once
// - how to split requests up among peers
type SinglePlanner interface {
	GeneratePlan(ctx context.Context, targetRoot cid.Cid, targetSelector ipld.Node, potentialRequests <-chan PotentialRequest) <-chan TransportPlan
}

func NewSimplePlanner(singlePlanner SinglePlanner) *SimplePlanner {
	return &SimplePlanner{singlePlanner}
}

type SimplePlanner struct {
	singlePlanner SinglePlanner
}

func (p *SimplePlanner) RegisterRecordInterpreter(minRange multicodec.Code, maxRange multicodec.Code, transportMultiplier PolicyWeight, interpreter RoutingRecordInterpreter) error {

	// what to do here -- record in some kind of map
	// make sure there is not conflict of ranges
	panic("not implemented")

}

func (p *SimplePlanner) PlanRequests(ctx context.Context, root cid.Cid, selector ipld.Node, policyPreference PolicyPreferences, routingResults <-chan contentrouting.RoutingRecord) <-chan TransportPlan {
	// what to do here (initial implementation, only supports a single request per transfer
	// consume content routing channel
	// for each record, identify the routing record interpreter
	// interpret it and score
	// pass the weigted routing record to the single planner
	// for the initial implementation, we can just pass the single planner result as the return value, as we aren't generating
	// multiple request iterations
	panic("not implemented")

}
