package planning

import "sync"

// Board keeps track of the state machine of transfers.
// requests transition from possible->pending->{failed, complete}
type Board struct {
	lock     sync.Mutex
	Possible []*TransportRequest
	Pending  []*TransportRequest
	Failed   []*TransportRequest
	Complete []*TransportRequest
}

// NewBoard initializes a new Board for planning requests
func NewBoard() *Board {
	return &Board{
		Possible: make([]*TransportRequest, 0),
		Pending:  make([]*TransportRequest, 0),
		Failed:   make([]*TransportRequest, 0),
		Complete: make([]*TransportRequest, 0),
	}
}

// Reconcile accounts for a pending transport request resolving, either successfully or not
func (b *Board) Reconcile(r *TransportRequest, success bool) {
	b.lock.Lock()
	defer b.lock.Unlock()
	// remove from pending. put in complete or failed.
	found := false
	for i, t := range b.Pending {
		if t == r {
			b.Pending = append(b.Pending[0:i], b.Pending[i+1:]...)
			found = true
			break
		}
	}
	if found {
		if success {
			b.Complete = append(b.Complete, r)
		} else {
			b.Failed = append(b.Failed, r)
		}
	}
}

// Begin starts a possible transport request (moves it to pending)
func (b *Board) Begin(r *TransportRequest) {
	b.lock.Lock()
	defer b.lock.Unlock()
	found := false
	for i, t := range b.Possible {
		if t == r {
			b.Possible = append(b.Possible[0:i], b.Possible[i+1:]...)
			found = true
			break
		}
	}
	if found {
		b.Pending = append(b.Pending, r)
	}
}

// Active returns if there is active work ongoing or available from the board
func (b *Board) Active() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return !(len(b.Pending) == 0 && len(b.Possible) == 0)
}

// AddPossible tells the board about a new potential transfer
func (b *Board) AddPossible(r *TransportRequest) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.Possible = append(b.Possible, r)
}

// HighestScore determines the next most promissing transfer to attempt
// currently this is a very simple setup:
// weight increases for each successful transfer from that provider.
// weight decreses by 5 for each unsuccessful transfer from that provider.
// weight decreases by 1 for each in-progress transfer from that provider.
func (b *Board) HighestScore() *TransportRequest {
	b.lock.Lock()
	defer b.lock.Unlock()
	if len(b.Possible) == 0 {
		return nil
	}
	scores := make([]int, len(b.Possible))
	for i, t := range b.Possible {
		for _, g := range b.Complete {
			if providersEqual(g.RoutingProvider, t.RoutingProvider) {
				scores[i]++
			}
		}
		for _, b := range b.Failed {
			if providersEqual(b.RoutingProvider, t.RoutingProvider) {
				scores[i] -= 5
			}
		}
		for _, p := range b.Pending {
			if providersEqual(p.RoutingProvider, t.RoutingProvider) {
				scores[i]--
			}
		}
	}
	maxIndex := 0
	maxScore := scores[0]
	for i, v := range scores {
		if v > maxScore {
			maxScore = v
			maxIndex = i
		}
	}
	return b.Possible[maxIndex]
}

func providersEqual(a, b interface{}) bool {
	type stringable interface {
		String() string
	}
	as, ok := a.(stringable)
	if !ok {
		return a == b
	}
	bs, ok := b.(stringable)
	if !ok {
		return false
	}
	return as.String() == bs.String()
}
