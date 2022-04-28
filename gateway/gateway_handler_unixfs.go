package gateway

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"time"

	files "github.com/ipfs/go-ipfs-files"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func (i *gatewayHandler) serveUnixFS(ctx context.Context, w http.ResponseWriter, r *http.Request, resolvedPath Resolved, contentPath Path, begin time.Time, logger *zap.SugaredLogger) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "gateway.serveUnixFS", trace.WithAttributes(attribute.String("path", resolvedPath.String())))
	defer span.End()
	// Handling UnixFS
	dr, err := i.api.Unixfs().Get(ctx, resolvedPath)
	if err != nil {
		webError(w, "ipfs cat "+html.EscapeString(contentPath.String()), err, http.StatusNotFound)
		return
	}
	defer dr.Close()

	// Handling Unixfs file
	if f, ok := dr.(files.File); ok {
		logger.Debugw("serving unixfs file", "path", contentPath)
		i.serveFile(ctx, w, r, resolvedPath, contentPath, f, begin)
		return
	}

	// Handling Unixfs directory
	dir, ok := dr.(files.Directory)
	if !ok {
		internalWebError(w, fmt.Errorf("unsupported UnixFS type"))
		return
	}
	logger.Debugw("serving unixfs directory", "path", contentPath)
	i.serveDirectory(ctx, w, r, resolvedPath, contentPath, dir, begin, logger)
}
