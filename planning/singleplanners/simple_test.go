package singleplanners_test

import (
	"context"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/ipfs-shipyard/w3rc/planning"
	"github.com/ipfs-shipyard/w3rc/planning/singleplanners"
	"github.com/ipfs-shipyard/w3rc/testutil"
	"github.com/ipfs/go-cid"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"github.com/multiformats/go-multicodec"
	"github.com/stretchr/testify/require"
)

func TestSimpleSinglePlanner(t *testing.T) {
	root := testutil.GenerateCids(1)[0]
	selector := selectorparse.CommonSelector_ExploreAllRecursively
	testCases := map[string]struct {
		minPolicyScore planning.PolicyScore
		maxWaitTime    time.Duration
		steps          []step
		expectedResult planning.TransportPlan
	}{
		"received result over min before maxWaitTime": {
			minPolicyScore: 0.1,
			maxWaitTime:    500 * time.Millisecond,
			steps: []step{
				receiveRequest{planning.PotentialRequest{
					PolicyScore:   0.05,
					RoutingRecord: mockRoutingRecord{0x1, "provider below min", "payload below min"},
				}},
				timerAdvance{100 * time.Millisecond},
				receiveRequest{planning.PotentialRequest{
					PolicyScore:   0.2,
					RoutingRecord: mockRoutingRecord{0x2, "provider above min", "payload above min"},
				}},
			},
			expectedResult: planning.TransportPlan{
				TransportRequests: []planning.TransportRequest{
					{
						Codec:           0x2,
						Root:            cidlink.Link{Cid: root},
						Selector:        selector,
						RoutingProvider: "provider above min",
						RoutingPayload:  "payload above min",
					},
				},
			},
		},
		"received results below min, maxWaitTime hit, best record first": {
			minPolicyScore: 0.1,
			maxWaitTime:    500 * time.Millisecond,
			steps: []step{
				receiveRequest{planning.PotentialRequest{
					PolicyScore:   0.05,
					RoutingRecord: mockRoutingRecord{0x1, "provider below min", "payload below min"},
				}},
				timerAdvance{100 * time.Millisecond},
				receiveRequest{planning.PotentialRequest{
					PolicyScore:   0.02,
					RoutingRecord: mockRoutingRecord{0x2, "provider below min worse", "payload below min worse"},
				}},
				timerAdvance{500 * time.Millisecond},
			},
			expectedResult: planning.TransportPlan{
				TransportRequests: []planning.TransportRequest{
					{
						Codec:           0x1,
						Root:            cidlink.Link{Cid: root},
						Selector:        selector,
						RoutingProvider: "provider below min",
						RoutingPayload:  "payload below min",
					},
				},
			},
		},
		"received results below min, maxWaitTime hit, best record last": {
			minPolicyScore: 0.1,
			maxWaitTime:    500 * time.Millisecond,
			steps: []step{
				receiveRequest{planning.PotentialRequest{
					PolicyScore:   0.02,
					RoutingRecord: mockRoutingRecord{0x1, "provider below min worse", "payload below min worse"},
				}},
				timerAdvance{100 * time.Millisecond},
				receiveRequest{planning.PotentialRequest{
					PolicyScore:   0.05,
					RoutingRecord: mockRoutingRecord{0x2, "provider below min", "payload below min"},
				}},
				timerAdvance{500 * time.Millisecond},
			},
			expectedResult: planning.TransportPlan{
				TransportRequests: []planning.TransportRequest{
					{
						Codec:           0x2,
						Root:            cidlink.Link{Cid: root},
						Selector:        selector,
						RoutingProvider: "provider below min",
						RoutingPayload:  "payload below min",
					},
				},
			},
		},
		"maxWaitTime hit, no records received": {
			minPolicyScore: 0.1,
			maxWaitTime:    500 * time.Millisecond,
			steps: []step{
				timerAdvance{600 * time.Millisecond},
			},
			expectedResult: planning.TransportPlan{},
		},
		"context cancelled, no records received": {
			minPolicyScore: 0.1,
			maxWaitTime:    500 * time.Millisecond,
			steps: []step{
				endContext{},
			},
			expectedResult: planning.TransportPlan{},
		},
		"context cancelled, records received, not returned": {
			minPolicyScore: 0.1,
			maxWaitTime:    500 * time.Millisecond,
			steps: []step{
				receiveRequest{planning.PotentialRequest{
					PolicyScore:   0.05,
					RoutingRecord: mockRoutingRecord{0x2, "provider below min", "payload below min"},
				}},
				endContext{},
			},
			expectedResult: planning.TransportPlan{},
		},
	}
	for testCase, data := range testCases {
		t.Run(testCase, func(t *testing.T) {
			clock := clock.NewMock()
			testCtx, testCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer testCancel()
			ssp := singleplanners.NewSimpleSinglePlanner(data.minPolicyScore, data.maxWaitTime, clock)
			potentialRequests := make(chan planning.PotentialRequest, len(data.steps))
			planCtx, planCancel := context.WithCancel(testCtx)
			defer planCancel()
			resultChan := ssp.GeneratePlan(planCtx, root, selector, potentialRequests)
			for i, s := range data.steps {
				s.visit(t, planCtx, planCancel, potentialRequests, clock)
				if i != len(data.steps)-1 {
					select {
					case <-resultChan:
						require.FailNow(t, "received results prematurely")
					default:
					}
				}
			}
			select {
			case <-testCtx.Done():
				require.FailNow(t, "did not receive results")
			case receivedResults := <-resultChan:
				require.Equal(t, data.expectedResult, receivedResults)
			}
		})
	}
}

type step interface {
	visit(t *testing.T, ctx context.Context, cancelFn context.CancelFunc, recordChan chan<- planning.PotentialRequest, clock *clock.Mock)
}

type receiveRequest struct {
	potentialRequest planning.PotentialRequest
}

func (rr receiveRequest) visit(t *testing.T, ctx context.Context, cancelFn context.CancelFunc, potentialRequests chan<- planning.PotentialRequest, clock *clock.Mock) {
	select {
	case <-ctx.Done():
		require.FailNow(t, "unable to input record")
	case potentialRequests <- rr.potentialRequest:
	}
}

type timerAdvance struct {
	amount time.Duration
}

func (ta timerAdvance) visit(t *testing.T, ctx context.Context, cancelFn context.CancelFunc, potentialRequests chan<- planning.PotentialRequest, clock *clock.Mock) {
	clock.Add(ta.amount)
}

type endContext struct{}

func (endContext) visit(t *testing.T, ctx context.Context, cancelFn context.CancelFunc, potentialRequests chan<- planning.PotentialRequest, clock *clock.Mock) {
	cancelFn()
}

type mockRoutingRecord struct {
	codec    multicodec.Code
	provider interface{}
	payload  interface{}
}

func (mrr mockRoutingRecord) Request() cid.Cid {
	panic("not implemented") // TODO: Implement
}

func (mrr mockRoutingRecord) Protocol() multicodec.Code {
	return mrr.codec
}

func (mrr mockRoutingRecord) Provider() interface{} {
	return mrr.provider
}

func (mrr mockRoutingRecord) Payload() interface{} {
	return mrr.payload
}
