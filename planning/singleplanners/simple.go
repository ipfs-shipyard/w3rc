package singleplanners

import (
	"context"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/ipfs-shipyard/w3rc/planning"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
)

func NewSimpleSinglePlanner(minPolicyScore planning.PolicyScore, maxWaitTime time.Duration, clock clock.Clock) planning.SinglePlanner {
	return &simpleSinglePlanner{minPolicyScore, maxWaitTime, clock}
}

type simpleSinglePlanner struct {
	minPolicyScore planning.PolicyScore
	maxWaitTime    time.Duration
	clock          clock.Clock
}

func (sp *simpleSinglePlanner) makeTransportPlan(ctx context.Context, timer *clock.Timer, targetRoot cid.Cid, targetSelector ipld.Node, potentialRequests <-chan planning.PotentialRequest) planning.TransportPlan {
	var bestCandidate planning.PotentialRequest
	for {
		select {
		case <-timer.C:
			if bestCandidate.RoutingRecord != nil {
				return planning.NewSimpleTransportPlan(targetRoot, targetSelector, bestCandidate.RoutingRecord)
			}
			return planning.TransportPlan{}
		case candidate := <-potentialRequests:
			// a candidate is the best if none exists yet or the policy score is better
			if bestCandidate.RoutingRecord == nil || candidate.PolicyScore > bestCandidate.PolicyScore {
				bestCandidate = candidate
			}
			if bestCandidate.PolicyScore > sp.minPolicyScore {
				return planning.NewSimpleTransportPlan(targetRoot, targetSelector, bestCandidate.RoutingRecord)
			}
		case <-ctx.Done():
			return planning.TransportPlan{}
		}
	}
}

func (sp *simpleSinglePlanner) GeneratePlan(ctx context.Context, targetRoot cid.Cid, targetSelector ipld.Node, potentialRequests <-chan planning.PotentialRequest) <-chan planning.TransportPlan {
	// for here, just read values until either max time is reached or min policy score is met,
	// then generate a transport plan with a single request
	// generate the transport request frim targetRoot & targetSelector + routing record
	transportPlanChan := make(chan planning.TransportPlan, 1)
	timer := sp.clock.Timer(sp.maxWaitTime)
	go func() {
		tp := sp.makeTransportPlan(ctx, timer, targetRoot, targetSelector, potentialRequests)
		transportPlanChan <- tp
	}()
	return transportPlanChan
}
