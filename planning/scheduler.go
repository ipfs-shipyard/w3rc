package planning

import (
	"context"
	"fmt"
	"time"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
)

// ErrNoTransport is an error option a transport plan my emit when no transports are currently possible
var ErrNoTransport = fmt.Errorf("no routes available")

// Scheduler provides an interface for deciding which potential transports to begin when.
type Scheduler interface {
	// plan the fetch of a root+selector of data given a set of learned routing records.
	Schedule(ctx context.Context, root cid.Cid, selector ipld.Node, potentialTransports <-chan contentrouting.RoutingRecord) <-chan TransportPlan
	// Indicate that a transfer in the schedule has started
	Begin(r *TransportRequest)
	// Indicate that a transfer in the schedule has completed
	Reconcile(r *TransportRequest, success bool)
}

// NewSimpleScheduler creates an instance of a SimpleScheduler
func NewSimpleScheduler() Scheduler {
	return &SimpleScheduler{
		board: NewBoard(),
	}
}

// A SimpleScheduler will attempt to generate a tansport plan based on a board tracking active requests
type SimpleScheduler struct {
	board    *Board
	plan     chan TransportPlan
	selector ipld.Node
}

// Schedule begins a schedule to get a cid+selector given a stream of potential routes.
// SimpleScheduler only handles one Schedule call concurrently.
func (s *SimpleScheduler) Schedule(ctx context.Context, root cid.Cid, selector ipld.Node, potentialTransports <-chan contentrouting.RoutingRecord) <-chan TransportPlan {
	s.plan = make(chan TransportPlan)
	s.selector = selector

	go s.background(ctx, potentialTransports)
	return s.plan
}

func (s *SimpleScheduler) background(ctx context.Context, potentialTransports <-chan contentrouting.RoutingRecord) {
	defer close(s.plan)
	// todo: periods should change based on board state:
	// * no active: short
	// * pending: based on how recently progress has come out of those active transfers
	// TODO: also need feedback loop - if the attempted option fails we should try the next immediately.
	ticker := time.NewTicker(100 * time.Millisecond) //todo: make configurable
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Infof("schedule closed by context: %w", ctx.Err())
			return
		case newOption, more := <-potentialTransports:
			if !more {
				potentialTransports = nil
				continue
			}
			if newOption.Protocol() == contentrouting.RoutingErrorProtocol {
				log.Warnf("got error routing record: %s", newOption.Payload())
				continue
			}

			option := TransportRequest{
				Codec:           newOption.Protocol(),
				Root:            cidlink.Link{Cid: newOption.Request()},
				Selector:        s.selector, // todo: sub-selectors
				RoutingProvider: newOption.Provider(),
				RoutingPayload:  newOption.Payload(),
			}
			s.board.AddPossible(&option)
		case <-ticker.C:
			s.emitNext()
		}
		if potentialTransports == nil && !s.board.Active() {
			return
		}
	}
}

func (s *SimpleScheduler) emitNext() {
	best := s.board.HighestScore()
	if best != nil {
		s.plan <- TransportPlan{
			TransportRequests: []*TransportRequest{best},
			Error:             nil,
		}
	} else if len(s.board.Pending) == 0 {
		s.plan <- TransportPlan{
			TransportRequests: nil,
			Error:             ErrNoTransport,
		}
	}
}

// Begin is called to tell the scheduler that a transport request has begun
func (s *SimpleScheduler) Begin(r *TransportRequest) {
	s.board.Begin(r)
}

// Reconcile is called to tell that a transport request has finished
func (s *SimpleScheduler) Reconcile(r *TransportRequest, success bool) {
	s.board.Reconcile(r, success)
}
