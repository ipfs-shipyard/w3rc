package planning

// Note: this is a future-looking class for a more extensible planning policy.
// It is not currently used.

import (
	"context"
	"errors"
	"time"

	"github.com/filecoin-project/indexer-reference-provider/metadata"
	v0 "github.com/filecoin-project/storetheindex/api/v0"
	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	"github.com/multiformats/go-multicodec"
)

var log = logging.Logger("w3rc-planning")

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

func NewSimpleSinglePlanner(minPolicyScore PolicyScore, maxWaitTime time.Duration) SinglePlanner {
	return &simpleSinglePlanner{minPolicyScore, maxWaitTime}
}

type simpleSinglePlanner struct {
	minPolicyScore PolicyScore
	maxWaitTime    time.Duration
}

func (sp *simpleSinglePlanner) GeneratePlan(ctx context.Context, targetRoot cid.Cid, targetSelector ipld.Node, potentialRequests <-chan PotentialRequest) <-chan TransportPlan {
	// for here, just read values until either max time is reached or min policy score is met,
	// then generate a transport plan with a single request
	// generate the transport request frim targetRoot & targetSelector + routing record
	panic("not implemented")

}

var _ RoutingRecordInterpreter = (*FilecoinV1RecordInterpreter)(nil)

type FilecoinV1RecordInterpreter struct {
}

func (fri FilecoinV1RecordInterpreter) Interpret(record contentrouting.RoutingRecord, policies []Policy) (PolicyResults, error) {

	// decode the record (or error) -- use metadata from filecoin
	// check for free or paid policy
	// return PolicyResults that when given "prefer_free" returns 1 if retrieval is free or zero if its paid

	if record.Protocol() == contentrouting.RoutingErrorProtocol {
		err, ok := record.Payload().(error)
		if !ok {
			return nil, errors.New("routing record payload not match expected type: error")
		}
		return nil, err
	}

	data, ok := record.Payload().([]byte)
	if !ok {
		return nil, errors.New("filecoin v1 routing record payload does not match expected type: []byte")
	}
	rm := v0.Metadata{
		ProtocolID: record.Protocol(),
		Data:       data,
	}

	dtm, err := metadata.FromIndexerMetadata(rm)
	if err != nil {
		return nil, err
	}

	fv1d, err := metadata.DecodeFilecoinV1Data(dtm)
	if err != nil {
		return nil, err
	}

	// TODO: How should policies argument impact the returned results?

	return &simplePolicyResults{isFree: fv1d.IsFree}, nil
}

var _ PolicyResults = (*simplePolicyResults)(nil)

type simplePolicyResults struct {
	isFree bool
}

// TODO: separating policies in their own package means we cannot use the PreferFree policy here due to cyclic dependency.
// Consider restructuring packages.
var preferFreePolicyName = PolicyName("prefer_free")

func (s simplePolicyResults) Score(name PolicyName) PolicyScore {
	if s.isFree && name == preferFreePolicyName {
		return 1
	}
	return 0
}
