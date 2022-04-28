package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ipfs/go-fetcher"
	gocar "github.com/ipld/go-car/v2"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	basicnode "github.com/ipld/go-ipld-prime/node/basic"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// serveCAR returns a CAR stream for specific DAG+selector
func (i *gatewayHandler) serveCAR(ctx context.Context, w http.ResponseWriter, r *http.Request, resolvedPath Resolved, contentPath Path, carVersion string, begin time.Time) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "gateway.serveCar", trace.WithAttributes(attribute.String("path", resolvedPath.String())))
	defer span.End()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	switch carVersion {
	case "": // noop, client does not care about version
	case "1": // noop, we support this
	default:
		err := fmt.Errorf("only version=1 is supported")
		webError(w, "unsupported CAR version", err, http.StatusBadRequest)
		return
	}
	rootCid := resolvedPath.Cid()

	// Set Content-Disposition
	name := rootCid.String() + ".car"
	setContentDispositionHeader(w, name, "attachment")

	// Weak Etag W/ because we can't guarantee byte-for-byte identical  responses
	// (CAR is streamed, and in theory, blocks may arrive from datastore in non-deterministic order)
	etag := `W/` + getEtag(r, rootCid)
	w.Header().Set("Etag", etag)

	// Finish early if Etag match
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Make it clear we don't support range-requests over a car stream
	// Partial downloads and resumes should be handled using
	// IPLD selectors: https://github.com/ipfs/go-ipfs/issues/8769
	w.Header().Set("Accept-Ranges", "none")

	// Explicit Cache-Control to ensure fresh stream on retry.
	// CAR stream could be interrupted, and client should be able to resume and get full response, not the truncated one
	w.Header().Set("Cache-Control", "no-cache, no-transform")

	w.Header().Set("Content-Type", "application/vnd.ipld.car; version=1")
	w.Header().Set("X-Content-Type-Options", "nosniff") // no funny business in the browsers :^)

	// Same go-car settings as dag.export command
	ls := i.api.NewSession(ctx)
	f := i.api.FetcherForSession(ls)
	rootLink := cidlink.Link{Cid: rootCid}
	// TODO: support selectors passed as request param: https://github.com/ipfs/go-ipfs/issues/8769
	if err := f.NodeMatching(ctx, basicnode.NewLink(rootLink), selectorparse.CommonSelector_ExploreAllRecursively, func(result fetcher.FetchResult) error { return nil }); err != nil {
		webError(w, "ipfs car get "+rootCid.String(), err, http.StatusInternalServerError)
		return
	}

	if _, err := gocar.TraverseV1(ctx, ls, rootCid, selectorparse.CommonSelector_ExploreAllRecursively, w); err != nil {
		w.Header().Set("X-Stream-Error", err.Error())
		return
	}

	// Update metrics
	i.carStreamGetMetric.WithLabelValues(contentPath.Namespace()).Observe(time.Since(begin).Seconds())
}
