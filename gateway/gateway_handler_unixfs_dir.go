package gateway

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-fetcher"
	gopath "github.com/ipfs/go-path"
	"github.com/ipld/go-ipld-prime"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// serveDirectory returns the best representation of UnixFS directory
//
// It will return index.html if present, or generate directory listing otherwise.
func (i *gatewayHandler) serveDirectory(ctx context.Context, w http.ResponseWriter, r *http.Request, resolvedPath Resolved, contentPath Path, dir ipld.Node, begin time.Time, logger *zap.SugaredLogger) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "gateway.serveDirectory", trace.WithAttributes(attribute.String("path", resolvedPath.String())))
	defer span.End()

	// HostnameOption might have constructed an IPNS/IPFS path using the Host header.
	// In this case, we need the original path for constructing redirects
	// and links that match the requested URL.
	// For example, http://example.net would become /ipns/example.net, and
	// the redirects and links would end up as http://example.net/ipns/example.net
	requestURI, err := url.ParseRequestURI(r.RequestURI)
	if err != nil {
		webError(w, "failed to parse request path", err, http.StatusInternalServerError)
		return
	}
	originalUrlPath := requestURI.Path

	// Check if directory has index.html, if so, serveFile
	if idx, err := dir.LookupByString("index.html"); err == nil {
		idxPath := JoinPath(resolvedPath, "index.html")
		// make sure we've loaded the index.
		ls := i.api.NewSession(ctx)
		fetchSession := i.api.FetcherForSession(ls)
		err := fetchSession.NodeMatching(ctx, idx, selectorparse.CommonSelector_ExploreAllRecursively, func(_ fetcher.FetchResult) error { return nil })
		if err != nil {
			internalWebError(w, err)
			return
		}

		cpath := contentPath.String()
		dirwithoutslash := cpath[len(cpath)-1] != '/'
		goget := r.URL.Query().Get("go-get") == "1"
		if dirwithoutslash && !goget {
			// See comment above where originalUrlPath is declared.
			suffix := "/"
			if r.URL.RawQuery != "" {
				// preserve query parameters
				suffix = suffix + "?" + r.URL.RawQuery
			}

			redirectURL := originalUrlPath + suffix
			logger.Debugw("serving index.html file", "to", redirectURL, "status", http.StatusFound, "path", idxPath)
			http.Redirect(w, r, redirectURL, http.StatusFound)
			return
		}

		logger.Debugw("serving index.html file", "path", idxPath)
		// write to request
		i.serveFile(ctx, w, r, resolvedPath, idxPath, idx, begin)
		return
	}

	// See statusResponseWriter.WriteHeader
	// and https://github.com/ipfs/go-ipfs/issues/7164
	// Note: this needs to occur before listingTemplate.Execute otherwise we get
	// superfluous response.WriteHeader call from prometheus/client_golang
	if w.Header().Get("Location") != "" {
		logger.Debugw("location moved permanently", "status", http.StatusMovedPermanently)
		w.WriteHeader(http.StatusMovedPermanently)
		return
	}

	// A HTML directory index will be presented, be sure to set the correct
	// type instead of relying on autodetection (which may fail).
	w.Header().Set("Content-Type", "text/html")

	// Generated dir index requires custom Etag (output may change between go-ipfs versions)
	dirEtag := getDirListingEtag(resolvedPath.Cid())
	w.Header().Set("Etag", dirEtag)

	if r.Method == http.MethodHead {
		logger.Debug("return as request's HTTP method is HEAD")
		return
	}

	// storage for directory listing
	var dirListing []directoryItem
	dirit := dir.MapIterator()
	for !dirit.Done() {
		size := "?"
		name, _, err := dirit.Next()
		if err != nil {
			internalWebError(w, err)
			return
		}
		// TODO: size
		//if s, err := dirit.Node().Size(); err == nil {
		// Size may not be defined/supported. Continue anyways.
		//	size = humanize.Bytes(uint64(s))
		//}
		nameStr, _ := name.AsString()

		resolved, err := ResolvePath(ctx, i.api, JoinPath(resolvedPath, nameStr))
		if err != nil {
			internalWebError(w, err)
			return
		}
		hash := resolved.Cid().String()

		// See comment above where originalUrlPath is declared.
		di := directoryItem{
			Size:      size,
			Name:      nameStr,
			Path:      gopath.Join([]string{originalUrlPath, nameStr}),
			Hash:      hash,
			ShortHash: shortHash(hash),
		}
		dirListing = append(dirListing, di)
	}

	// construct the correct back link
	// https://github.com/ipfs/go-ipfs/issues/1365
	var backLink string = originalUrlPath

	// don't go further up than /ipfs/$hash/
	pathSplit := gopath.SplitList(contentPath.String())
	switch {
	// keep backlink
	case len(pathSplit) == 3: // url: /ipfs/$hash

	// keep backlink
	case len(pathSplit) == 4 && pathSplit[3] == "": // url: /ipfs/$hash/

	// add the correct link depending on whether the path ends with a slash
	default:
		if strings.HasSuffix(backLink, "/") {
			backLink += "./.."
		} else {
			backLink += "/.."
		}
	}

	size := "?"
	//if s, err := dir.Size(); err == nil {
	// Size may not be defined/supported. Continue anyways.
	//	size = humanize.Bytes(uint64(s))
	//}

	hash := resolvedPath.Cid().String()

	// Gateway root URL to be used when linking to other rootIDs.
	// This will be blank unless subdomain or DNSLink resolution is being used
	// for this request.
	var gwURL string

	// Get gateway hostname and build gateway URL.
	if h, ok := r.Context().Value(GatewayHostnameKey).(string); ok {
		gwURL = "//" + h
	} else {
		gwURL = ""
	}

	//dnslink := hasDNSLinkOrigin(gwURL, contentPath.String())

	// See comment above where originalUrlPath is declared.
	tplData := listingTemplateData{
		GatewayURL:  gwURL,
		DNSLink:     false,
		Listing:     dirListing,
		Size:        size,
		Path:        contentPath.String(),
		Breadcrumbs: breadcrumbs(contentPath.String(), false),
		BackLink:    backLink,
		Hash:        hash,
	}

	logger.Debugw("request processed", "tplDataSize", size, "tplDataBackLink", backLink, "tplDataHash", hash)

	if err := listingTemplate.Execute(w, tplData); err != nil {
		internalWebError(w, err)
		return
	}

	// Update metrics
	i.unixfsGenDirGetMetric.WithLabelValues(contentPath.Namespace()).Observe(time.Since(begin).Seconds())
}

func getDirListingEtag(dirCid cid.Cid) string {
	return `"DirIndex-unknown_CID-` + dirCid.String() + `"`
}
