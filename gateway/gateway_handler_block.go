package gateway

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	basicnode "github.com/ipld/go-ipld-prime/node/basic"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// serveRawBlock returns bytes behind a raw block
func (i *gatewayHandler) serveRawBlock(ctx context.Context, w http.ResponseWriter, r *http.Request, resolvedPath Resolved, contentPath Path, begin time.Time) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "Gateway.ServeRawBlock", trace.WithAttributes(attribute.String("path", resolvedPath.String())))
	defer span.End()
	blockCid := resolvedPath.Cid()

	ls := i.api.NewSession(ctx)
	f := i.api.FetcherForSession(ls)
	if _, err := f.BlockOfType(ctx, cidlink.Link{Cid: blockCid}, basicnode.Prototype.Any); err != nil {
		webError(w, "ipfs block get "+blockCid.String(), err, http.StatusInternalServerError)
	}

	_, blockBytes, err := ls.LoadPlusRaw(ipld.LinkContext{Ctx: ctx}, cidlink.Link{Cid: blockCid}, basicnode.Prototype.Any)
	if err != nil {
		webError(w, "ipfs block get "+blockCid.String(), err, http.StatusInternalServerError)
		return
	}

	// Set Content-Disposition
	name := blockCid.String() + ".bin"
	setContentDispositionHeader(w, name, "attachment")

	// Set remaining headers
	modtime := addCacheControlHeaders(w, r, contentPath, blockCid)
	w.Header().Set("Content-Type", "application/vnd.ipld.raw")
	w.Header().Set("X-Content-Type-Options", "nosniff") // no funny business in the browsers :^)

	// ServeContent will take care of
	// If-None-Match+Etag, Content-Length and range requests
	_, dataSent, _ := ServeContent(w, r, name, modtime, bytes.NewReader(blockBytes))

	if dataSent {
		// Update metrics
		i.rawBlockGetMetric.WithLabelValues(contentPath.Namespace()).Observe(time.Since(begin).Seconds())
	}
}
