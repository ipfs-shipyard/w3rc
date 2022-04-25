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
	getCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	records := s.router.FindProviders(getCtx, root)
	plan := s.scheduler.Schedule(getCtx, root, selector, records)
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
				s.scheduler.Begin(tr)
				if err := s.mux.Add(getCtx, tr); err != nil {
					s.scheduler.Reconcile(tr, false)
					log.Warnf("could not honor transport req: %s", err)
					continue
				}
			}
		case transportEvent, more := <-work:
			if transportEvent.Event == exchange.ErrorEvent {
				log.Warnf("error in transport: %s", transportEvent.State)
				continue
			}
			if transportEvent.State == exchange.FailureEvent {
				s.scheduler.Reconcile(transportEvent.Source, false)
			} else if transportEvent.State == exchange.SuccessEvent {
				s.scheduler.Reconcile(transportEvent.Source, true)
			}

			if !more {
				// if we have everything, things are good.
				link := cidlink.Link{Cid: root}
				// TODO: this isn't enough to know that we've actually loaded the full selector, just that
				// the mux has ended without an error. we need to know we can load the full requested selector
				// to conclude that work is done.
				return s.ls.Load(ipld.LinkContext{Ctx: getCtx}, link, basicnode.Prototype.Any)
			}
		}
	}
	return nil, nil
}

func (s *simpleSession) Close() error {
	return nil
}
