package exchange

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ipfs-shipyard/w3rc/planning"
	"github.com/multiformats/go-multicodec"
)

var ErrUnknownCodec = errors.New("unknown codec")

type MuxEvent struct {
	Source *planning.TransportRequest
	EventData
}

type ExchangeMux struct {
	knownCodecs map[multicodec.Code]Exchange
	mux         chan MuxEvent
	wg          sync.WaitGroup
	started     sync.Once
}

func DefaultMux() *ExchangeMux {
	em := ExchangeMux{
		knownCodecs: make(map[multicodec.Code]Exchange),
		mux:         make(chan MuxEvent, 0),
		wg:          sync.WaitGroup{},
	}
	em.wg.Add(1)
	return &em
}

func (e *ExchangeMux) start() {
	e.wg.Done()
}

func (e *ExchangeMux) forward(tr *planning.TransportRequest, c <-chan EventData) {
	for n := range c {
		e.mux <- MuxEvent{tr, n}
	}
	e.wg.Done()
}

func (e *ExchangeMux) Register(ex Exchange) error {
	e.knownCodecs[ex.Code()] = ex
	return nil
}

func (e *ExchangeMux) Add(ctx context.Context, tr *planning.TransportRequest) error {
	ex, ok := e.knownCodecs[tr.Codec]
	if !ok {
		fmt.Printf("err unknown codec.\n")
		return ErrUnknownCodec
	}
	evs := ex.RequestData(ctx, tr.Root, tr.Selector, tr.RoutingProvider, tr.RoutingPayload)
	e.wg.Add(1)
	e.started.Do(e.start)
	go e.forward(tr, evs)

	return nil
}

func (e *ExchangeMux) Subscribe() chan MuxEvent {
	go func() {
		e.wg.Wait()
		close(e.mux)
	}()
	return e.mux
}
