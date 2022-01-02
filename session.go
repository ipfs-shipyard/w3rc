package w3rc

import (
	"context"
	"fmt"

	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs-shipyard/w3rc/exchange"
	"github.com/ipfs-shipyard/w3rc/planning"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/basicnode"
)

type simpleSession struct {
	ls        ipld.LinkSystem
	router    contentrouting.Routing
	scheduler planning.Scheduler
	mux       *exchange.ExchangeMux
}

func (s *simpleSession) Get(ctx context.Context, root cid.Cid, selector datamodel.Node) (ipld.Node, error) {
	records := s.router.FindProviders(ctx, root)
	plan := s.scheduler.Schedule(ctx, root, selector, records)
	work := s.mux.Subscribe()

	workDone := false
	for !workDone {
		select {
		case nextPlan, more := <-plan:
			if !more {
				return nil, fmt.Errorf("no provider found")
			}
			if nextPlan.Error != nil {
				log.Warnf("planning error: %s\n", nextPlan.Error)
				continue
			}
			for _, tr := range nextPlan.TransportRequests {
				if err := s.mux.Add(ctx, tr); err != nil {
					s.scheduler.Begin(tr)
					s.scheduler.Reconcile(tr, false)
					log.Warnf("could not honor transport req: %s\n", err)
					continue
				}
				s.scheduler.Begin(tr)
			}
		case transportEvent, more := <-work:
			if transportEvent.Event == exchange.ErrorEvent {
				log.Warnf("error in transport: %s\n", transportEvent.State)
				continue
			}
			fmt.Printf("got transport event: %s / %d\n", transportEvent.State, transportEvent.Event)
			if transportEvent.State == exchange.FailureEvent {
				s.scheduler.Reconcile(transportEvent.Source, false)
			} else if transportEvent.State == exchange.SuccessEvent {
				s.scheduler.Reconcile(transportEvent.Source, true)
			}

			if !more {
				// if we have everything, things are good.
				link := cidlink.Link{Cid: root}
				node, err := s.ls.Load(ipld.LinkContext{Ctx: ctx}, link, basicnode.Prototype.Any)
				return node, err
			}
		}
	}
	return nil, nil
}

func (s *simpleSession) Close() error {
	return nil
}
