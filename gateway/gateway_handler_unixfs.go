package gateway

import (
	"context"
	"html"
	"net/http"
	"time"

	"github.com/ipfs/go-fetcher"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
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
	err := fetchSession.NodeMatching(ctx, basicnode.NewLink(cidlink.Link{Cid: resolvedPath.Cid()}), selectorparse.CommonSelector_MatchPoint, func(_ fetcher.FetchResult) error { return nil })
	node, err := ls.Load(ipld.LinkContext{Ctx: ctx}, cidlink.Link{Cid: resolvedPath.Cid()}, basicnode.Prototype.Any)
	if err != nil {
		webError(w, "ipfs cat "+html.EscapeString(contentPath.String()), err, http.StatusNotFound)
	}

	// Handling Unixfs file
	if node.Kind() == ipld.Kind_Bytes {
		logger.Debugw("serving unixfs file", "path", contentPath)
		i.serveFile(ctx, w, r, resolvedPath, contentPath, node, begin)
		return
	}

	// Handling Unixfs directory
	logger.Debugw("serving unixfs directory", "path", contentPath)
	i.serveDirectory(ctx, w, r, resolvedPath, contentPath, node, begin, logger)
}
