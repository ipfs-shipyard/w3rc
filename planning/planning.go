package planning

import (
	"context"
	"time"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs/go-cid"
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
	TransportRequests []TransportRequest
}

type PolicyName string

type Policy interface {
	Name() PolicyName
}

type PolicyResults interface {
	// must be in range from zero to 1, will get dropped otherwise
	// should return zero for policies that are unrecognized
	Score(PolicyName) PolicyScore
}

type PolicyWeight float64
type PolicyPreferences struct {
	preferences map[PolicyWeight]Policy
}

type PolicyScore float64

func (p *PolicyPreferences) WeightedScore(results PolicyResults, transportMultipler PolicyWeight) PolicyScore {
	score := PolicyScore(0)
	for weight, policy := range p.preferences {
		pscore := results.Score(policy.Name())
		if pscore < 0 || pscore > 1 {
			continue
		}
		score += pscore * PolicyScore(weight)
	}
	score *= PolicyScore(transportMultipler)
	return score
}

func (p *PolicyPreferences) AddPolicy(weight PolicyWeight, policy Policy) {
	p.preferences[weight] = policy
}

func (p *PolicyPreferences) Policies() []Policy {
	policies := make([]Policy, 0, len(p.preferences))
	for _, policy := range p.preferences {
		policies = append(policies, policy)
	}
	return policies
}

// RoutingRecordInterpreter interprets records for a given multicodec range
type RoutingRecordInterpreter interface {
	Interpret(record contentrouting.RoutingRecord, policies []Policy) (PolicyResults, error)
}

type PotentialRequest struct {
	PolicyScore
	contentrouting.RoutingRecord
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

// The Planner interface translates routing results to exchange requests.
// It's job is to figure out how to get content we found quickly and with least costs
type Planner interface {
	RegisterRecordInterpreter(minRange multicodec.Code, maxRange multicodec.Code, transportMultiplier PolicyWeight, interpreter RoutingRecordInterpreter) error
	PlanRequests(ctx context.Context, root cid.Cid, selector ipld.Node, policyPreference PolicyPreferences, routingResults <-chan contentrouting.RoutingRecord) <-chan TransportPlan
}

func NewSimplePlanner(singlePlanner SinglePlanner) Planner {
	return &simplePlanner{singlePlanner}
}

type simplePlanner struct {
	singlePlanner SinglePlanner
}

func (p *simplePlanner) RegisterRecordInterpreter(minRange multicodec.Code, maxRange multicodec.Code, transportMultiplier PolicyWeight, interpreter RoutingRecordInterpreter) error {

	// what to do here -- record in some kind of map
	// make sure there is not conflict of ranges
	panic("not implemented")

}

func (p *simplePlanner) PlanRequests(ctx context.Context, root cid.Cid, selector ipld.Node, policyPreference PolicyPreferences, routingResults <-chan contentrouting.RoutingRecord) <-chan TransportPlan {
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

type PreferFree struct{}

func (fp PreferFree) Name() PolicyName { return "prefer_free" }

var _ Policy = PreferFree{}

type FilecoinV1RecordInterpreter struct {
}

func (fri FilecoinV1RecordInterpreter) Interpret(record contentrouting.RoutingRecord, policies []Policy) (PolicyResults, error) {
	panic("not implemented")

	// decode the record (or error) -- use metadata from filecoin
	// check for free or paid policy
	// return PolicyResults that when given "prefer_free" returns 1 if retrieval is free or zero if its paid
}
