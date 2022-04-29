package gateway

import (
	"context"
	"html"
	"net/http"
	"time"

	"github.com/ipfs/go-fetcher"
	"github.com/ipfs/go-unixfsnode"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/linking"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/traversal/selector"
	"github.com/ipld/go-ipld-prime/traversal/selector/builder"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func (i *gatewayHandler) serveUnixFS(ctx context.Context, w http.ResponseWriter, r *http.Request, resolvedPath Resolved, contentPath Path, begin time.Time, logger *zap.SugaredLogger) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "gateway.serveUnixFS", trace.WithAttributes(attribute.String("path", resolvedPath.String())))
	defer span.End()
	// Handling UnixFS
	ls := i.api.NewSession(ctx)
	fetchSession := i.api.FetcherForSession(ls)
	ssb := builder.NewSelectorSpecBuilder(basicnode.Prototype.Any)
	selSpec := ssb.ExploreInterpretAs("unixfs", ssb.ExploreRecursive(selector.RecursionLimitDepth(1), ssb.ExploreAll(ssb.ExploreRecursiveEdge())))
	sel := selSpec.Node()
	err := fetchSession.NodeMatching(ctx, basicnode.NewLink(cidlink.Link{Cid: resolvedPath.Cid()}), sel, func(_ fetcher.FetchResult) error { return nil })
	if err != nil {
		webError(w, "ipfs cat "+html.EscapeString(contentPath.String()), err, http.StatusNotFound)
	}
	f := i.api.FetcherForSession(ls)
	proto, _ := f.PrototypeFromLink(cidlink.Link{Cid: resolvedPath.Cid()})
	node, err := ls.Load(ipld.LinkContext{Ctx: ctx}, cidlink.Link{Cid: resolvedPath.Cid()}, proto)
	if err != nil {
		webError(w, "ipfs cat "+html.EscapeString(contentPath.String()), err, http.StatusNotFound)
	}
	unode, err := unixfsnode.Reify(linking.LinkContext{Ctx: ctx}, node, ls)
	if err != nil {
		webError(w, "ipfs cat "+html.EscapeString(contentPath.String()), err, http.StatusNotFound)
	}

	// Handling Unixfs file
	if unode.Kind() == ipld.Kind_Bytes {
		logger.Debugw("serving unixfs file", "path", contentPath)
		i.serveFile(ctx, w, r, resolvedPath, contentPath, unode, begin)
		return
	}

	// Handling Unixfs directory
	logger.Debugf("resolved node is of type: %v", unode)
	logger.Debugw("serving unixfs directory", "path", contentPath)
	i.serveDirectory(ctx, w, r, resolvedPath, contentPath, unode, begin, logger)
}
