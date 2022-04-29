package gateway

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/textproto"
	"net/url"
	gopath "path"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-fetcher"
	logging "github.com/ipfs/go-log"
	resolver "github.com/ipfs/go-path/resolver"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	basicnode "github.com/ipld/go-ipld-prime/node/basic"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	ipfsPathPrefix        = "/ipfs/"
	ipnsPathPrefix        = "/ipns/"
	immutableCacheControl = "public, max-age=29030400, immutable"
)

var (
	onlyAscii = regexp.MustCompile("[[:^ascii:]]")
	noModtime = time.Unix(0, 0) // disables Last-Modified header if passed as modtime
)

var log = logging.Logger("gateway")

// HTML-based redirect for errors which can be recovered from, but we want
// to provide hint to people that they should fix things on their end.
var redirectTemplate = template.Must(template.New("redirect").Parse(`<!DOCTYPE html>
<html>
	<head>
		<meta charset="utf-8">
		<meta http-equiv="refresh" content="10;url={{.RedirectURL}}" />
		<link rel="canonical" href="{{.RedirectURL}}" />
	</head>
	<body>
		<pre>{{.ErrorMsg}}</pre><pre>(if a redirect does not happen in 10 seconds, use "{{.SuggestedPath}}" instead)</pre>
	</body>
</html>`))

type redirectTemplateData struct {
	RedirectURL   string
	SuggestedPath string
	ErrorMsg      string
}

// gatewayHandler is a HTTP handler that serves IPFS objects (accessible by default at /ipfs/<path>)
// (it serves requests like GET /ipfs/QmVRzPKPzNtSrEzBFm2UZfxmPAgnaLke4DMcerbsGGSaFe/link)
type gatewayHandler struct {
	config *GatewayConfig
	api    API

	// generic metrics
	firstContentBlockGetMetric *prometheus.HistogramVec
	unixfsGetMetric            *prometheus.SummaryVec // deprecated, use firstContentBlockGetMetric

	// response type metrics
	unixfsFileGetMetric   *prometheus.HistogramVec
	unixfsGenDirGetMetric *prometheus.HistogramVec
	carStreamGetMetric    *prometheus.HistogramVec
	rawBlockGetMetric     *prometheus.HistogramVec
}

// StatusResponseWriter enables us to override HTTP Status Code passed to
// WriteHeader function inside of http.ServeContent.  Decision is based on
// presence of HTTP Headers such as Location.
type statusResponseWriter struct {
	http.ResponseWriter
}

// Custom type for collecting error details to be handled by `webRequestError`
type requestError struct {
	Message    string
	StatusCode int
	Err        error
}

func (r *requestError) Error() string {
	return r.Err.Error()
}

func newRequestError(message string, err error, statusCode int) *requestError {
	return &requestError{
		Message:    message,
		Err:        err,
		StatusCode: statusCode,
	}
}

func (sw *statusResponseWriter) WriteHeader(code int) {
	// Check if we need to adjust Status Code to account for scheduled redirect
	// This enables us to return payload along with HTTP 301
	// for subdomain redirect in web browsers while also returning body for cli
	// tools which do not follow redirects by default (curl, wget).
	redirect := sw.ResponseWriter.Header().Get("Location")
	if redirect != "" && code == http.StatusOK {
		code = http.StatusMovedPermanently
		log.Debugw("subdomain redirect", "location", redirect, "status", code)
	}
	sw.ResponseWriter.WriteHeader(code)
}

// ServeContent replies to the request using the content in the provided ReadSeeker
// and returns the status code written and any error encountered during a write.
// It wraps http.ServeContent which takes care of If-None-Match+Etag,
// Content-Length and range requests.
func ServeContent(w http.ResponseWriter, req *http.Request, name string, modtime time.Time, content io.ReadSeeker) (int, bool, error) {
	ew := &errRecordingResponseWriter{ResponseWriter: w}
	http.ServeContent(ew, req, name, modtime, content)

	// When we calculate some metrics we want a flag that lets us to ignore
	// errors and 304 Not Modified, and only care when requested data
	// was sent in full.
	dataSent := ew.code/100 == 2 && ew.err == nil

	return ew.code, dataSent, ew.err
}

// errRecordingResponseWriter wraps a ResponseWriter to record the status code and any write error.
type errRecordingResponseWriter struct {
	http.ResponseWriter
	code int
	err  error
}

func (w *errRecordingResponseWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *errRecordingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err
}

// ReadFrom exposes errRecordingResponseWriter's underlying ResponseWriter to io.Copy
// to allow optimized methods to be taken advantage of.
func (w *errRecordingResponseWriter) ReadFrom(r io.Reader) (n int64, err error) {
	n, err = io.Copy(w.ResponseWriter, r)
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err
}

func newGatewaySummaryMetric(name string, help string) *prometheus.SummaryVec {
	summaryMetric := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: "ipfs",
			Subsystem: "http",
			Name:      name,
			Help:      help,
		},
		[]string{"gateway"},
	)
	if err := prometheus.Register(summaryMetric); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			summaryMetric = are.ExistingCollector.(*prometheus.SummaryVec)
		} else {
			log.Errorf("failed to register ipfs_http_%s: %v", name, err)
		}
	}
	return summaryMetric
}

func newGatewayHistogramMetric(name string, help string) *prometheus.HistogramVec {
	// We can add buckets as a parameter in the future, but for now using static defaults
	// suggested in https://github.com/ipfs/go-ipfs/issues/8441
	defaultBuckets := []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60}
	histogramMetric := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ipfs",
			Subsystem: "http",
			Name:      name,
			Help:      help,
			Buckets:   defaultBuckets,
		},
		[]string{"gateway"},
	)
	if err := prometheus.Register(histogramMetric); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			histogramMetric = are.ExistingCollector.(*prometheus.HistogramVec)
		} else {
			log.Errorf("failed to register ipfs_http_%s: %v", name, err)
		}
	}
	return histogramMetric
}

func newGatewayHandler(c *GatewayConfig, api API) *gatewayHandler {
	i := &gatewayHandler{
		config: c,
		api:    api,
		// Improved Metrics
		// ----------------------------
		// Time till the first content block (bar in /ipfs/cid/foo/bar)
		// (format-agnostic, across all response types)
		firstContentBlockGetMetric: newGatewayHistogramMetric(
			"gw_first_content_block_get_latency_seconds",
			"The time till the first content block is received on GET from the gateway.",
		),

		// Response-type specific metrics
		// ----------------------------
		// UnixFS: time it takes to return a file
		unixfsFileGetMetric: newGatewayHistogramMetric(
			"gw_unixfs_file_get_duration_seconds",
			"The time to serve an entire UnixFS file from the gateway.",
		),
		// UnixFS: time it takes to generate static HTML with directory listing
		unixfsGenDirGetMetric: newGatewayHistogramMetric(
			"gw_unixfs_gen_dir_listing_get_duration_seconds",
			"The time to serve a generated UnixFS HTML directory listing from the gateway.",
		),
		// CAR: time it takes to return requested CAR stream
		carStreamGetMetric: newGatewayHistogramMetric(
			"gw_car_stream_get_duration_seconds",
			"The time to GET an entire CAR stream from the gateway.",
		),
		// Block: time it takes to return requested Block
		rawBlockGetMetric: newGatewayHistogramMetric(
			"gw_raw_block_get_duration_seconds",
			"The time to GET an entire raw Block from the gateway.",
		),

		// Legacy Metrics
		// ----------------------------
		unixfsGetMetric: newGatewaySummaryMetric( // TODO: remove?
			// (deprecated, use firstContentBlockGetMetric instead)
			"unixfs_get_latency_seconds",
			"The time to receive the first UnixFS node on a GET from the gateway.",
		),
	}
	return i
}

/*
func parseIpfsPath(p string) (cid.Cid, string, error) {
	rootPath, err := ipfspath.ParsePath(p)
	if err != nil {
		return cid.Cid{}, "", err
	}

	// Check the path.
	rsegs := rootPath.Segments()
	if rsegs[0] != "ipfs" {
		return cid.Cid{}, "", fmt.Errorf("WritableGateway: only ipfs paths supported")
	}

	rootCid, err := cid.Decode(rsegs[1])
	if err != nil {
		return cid.Cid{}, "", err
	}

	return rootCid, ipfspath.Join(rsegs[2:]), nil
}
*/

func (i *gatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// the hour is a hard fallback, we don't expect it to happen, but just in case
	ctx, cancel := context.WithTimeout(r.Context(), time.Hour)
	defer cancel()
	r = r.WithContext(ctx)

	defer func() {
		if r := recover(); r != nil {
			log.Error("A panic occurred in the gateway handler!")
			log.Error(r)
			debug.PrintStack()
		}
	}()

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		i.getOrHeadHandler(w, r)
		return
	case http.MethodOptions:
		i.optionsHandler(w, r)
		return
	}

	errmsg := "Method " + r.Method + " not allowed: "
	status := http.StatusMethodNotAllowed
	errmsg = errmsg + "read only access"
	w.Header().Add("Allow", http.MethodGet)
	w.Header().Add("Allow", http.MethodHead)
	w.Header().Add("Allow", http.MethodOptions)
	http.Error(w, errmsg, status)
}

func (i *gatewayHandler) optionsHandler(w http.ResponseWriter, r *http.Request) {
	/*
		OPTIONS is a noop request that is used by the browsers to check
		if server accepts cross-site XMLHttpRequest (indicated by the presence of CORS headers)
		https://developer.mozilla.org/en-US/docs/Web/HTTP/Access_control_CORS#Preflighted_requests
	*/
	i.addUserHeaders(w) // return all custom headers (including CORS ones, if set)
}

func (i *gatewayHandler) getOrHeadHandler(w http.ResponseWriter, r *http.Request) {
	begin := time.Now()

	logger := log.With("from", r.RequestURI)
	logger.Debug("http request received")

	if err := handleUnsupportedHeaders(r); err != nil {
		webRequestError(w, err)
		return
	}

	if requestHandled := handleProtocolHandlerRedirect(w, r, logger); requestHandled {
		return
	}

	if err := handleServiceWorkerRegistration(r); err != nil {
		webRequestError(w, err)
		return
	}

	contentPath := NewPath(r.URL.Path)
	if requestHandled := handleSuperfluousNamespace(w, r, contentPath); requestHandled {
		return
	}

	// Resolve path to the final DAG node for the ETag
	resolvedPath, err := ResolvePath(r.Context(), i.api, contentPath)
	switch err {
	case nil:
	//case coreiface.ErrOffline:
	//	webError(w, "ipfs resolve -r "+debugStr(contentPath.String()), err, http.StatusServiceUnavailable)
	//	return
	default:
		// if Accept is text/html, see if ipfs-404.html is present
		if i.servePretty404IfPresent(w, r, contentPath) {
			logger.Debugw("serve pretty 404 if present")
			return
		}

		webError(w, "ipfs resolve -r "+debugStr(contentPath.String()), err, http.StatusNotFound)
		return
	}

	// Detect when explicit Accept header or ?format parameter are present
	responseFormat, formatParams, err := customResponseFormat(r)
	if err != nil {
		webError(w, "error while processing the Accept header", err, http.StatusBadRequest)
		return
	}
	trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("ResponseFormat", responseFormat))
	trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("ResolvedPath", resolvedPath.String()))

	// Detect when If-None-Match HTTP header allows returning HTTP 304 Not Modified
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		pathCid := resolvedPath.Cid()
		// need to check against both File and Dir Etag variants
		// because this inexpensive check happens before we do any I/O
		cidEtag := getEtag(r, pathCid)
		dirEtag := getDirListingEtag(pathCid)
		if etagMatch(inm, cidEtag, dirEtag) {
			// Finish early if client already has a matching Etag
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	if err := i.handleGettingFirstBlock(r, begin, contentPath, resolvedPath); err != nil {
		webRequestError(w, err)
		return
	}

	if err := i.setCommonHeaders(w, r, contentPath); err != nil {
		webRequestError(w, err)
		return
	}

	// Support custom response formats passed via ?format or Accept HTTP header
	switch responseFormat {
	case "": // The implicit response format is UnixFS
		logger.Debugw("serving unixfs", "path", contentPath)
		i.serveUnixFS(r.Context(), w, r, resolvedPath, contentPath, begin, logger)
		return
	case "application/vnd.ipld.raw":
		logger.Debugw("serving raw block", "path", contentPath)
		i.serveRawBlock(r.Context(), w, r, resolvedPath, contentPath, begin)
		return
	case "application/vnd.ipld.car":
		logger.Debugw("serving car stream", "path", contentPath)
		carVersion := formatParams["version"]
		i.serveCAR(r.Context(), w, r, resolvedPath, contentPath, carVersion, begin)
		return
	default: // catch-all for unsuported application/vnd.*
		err := fmt.Errorf("unsupported format %q", responseFormat)
		webError(w, "failed respond with requested content type", err, http.StatusBadRequest)
		return
	}
}

func (i *gatewayHandler) servePretty404IfPresent(w http.ResponseWriter, r *http.Request, contentPath Path) bool {
	resolved404Path, ctype, err := i.searchUpTreeFor404(r, contentPath)
	if err != nil {
		return false
	}

	ls := i.api.NewSession(r.Context())
	f := i.api.FetcherForSession(ls)
	sel := selectorparse.CommonSelector_ExploreAllRecursively
	if err := f.NodeMatching(r.Context(), basicnode.NewLink(cidlink.Link{Cid: resolved404Path.Cid()}), sel, func(result fetcher.FetchResult) error { return nil }); err != nil {
		return false
	}
	file, err := ls.Load(ipld.LinkContext{Ctx: r.Context()}, cidlink.Link{Cid: resolved404Path.Cid()}, basicnode.Prototype.Any)
	if err != nil {
		return false
	}

	byteReader, ok := file.(datamodel.LargeBytesNode)
	if ok {
		rs, err := byteReader.AsLargeBytes()
		if err == nil {
			size, err := rs.Seek(0, io.SeekEnd)
			if err == nil {
				_, _ = rs.Seek(0, io.SeekStart)
				log.Debugw("using pretty 404 file", "path", contentPath)
				w.Header().Set("Content-Type", ctype)
				w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
				w.WriteHeader(http.StatusNotFound)
				_, err = io.CopyN(w, rs, size)
				return err == nil
			}
		}
	}
	// fallback to non-large-bytes
	bytes, err := file.AsBytes()
	if err != nil {
		return false
	}
	size := len(bytes)

	log.Debugw("using pretty 404 file", "path", contentPath)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", strconv.FormatInt(int64(size), 10))
	w.WriteHeader(http.StatusNotFound)
	_, err = w.Write(bytes)
	return err == nil
}

func (i *gatewayHandler) addUserHeaders(w http.ResponseWriter) {
	for k, v := range i.config.HTTPHeaders {
		w.Header()[k] = v
	}
}

func addCacheControlHeaders(w http.ResponseWriter, r *http.Request, contentPath Path, fileCid cid.Cid) (modtime time.Time) {
	// Set Etag to based on CID (override whatever was set before)
	w.Header().Set("Etag", getEtag(r, fileCid))

	// Set Cache-Control and Last-Modified based on contentPath properties
	if contentPath.Mutable() {
		// mutable namespaces such as /ipns/ can't be cached forever

		/* For now we set Last-Modified to Now() to leverage caching heuristics built into modern browsers:
		 * https://github.com/ipfs/go-ipfs/pull/8074#pullrequestreview-645196768
		 * but we should not set it to fake values and use Cache-Control based on TTL instead */
		modtime = time.Now()

		// TODO: set Cache-Control based on TTL of IPNS/DNSLink: https://github.com/ipfs/go-ipfs/issues/1818#issuecomment-1015849462
		// TODO: set Last-Modified based on /ipns/ publishing timestamp?
	} else {
		// immutable! CACHE ALL THE THINGS, FOREVER! wolololol
		w.Header().Set("Cache-Control", immutableCacheControl)

		// Set modtime to 'zero time' to disable Last-Modified header (superseded by Cache-Control)
		modtime = noModtime

		// TODO: set Last-Modified? - TBD - /ipfs/ modification metadata is present in unixfs 1.5 https://github.com/ipfs/go-ipfs/issues/6920?
	}

	return modtime
}

// Set Content-Disposition if filename URL query param is present, return preferred filename
func addContentDispositionHeader(w http.ResponseWriter, r *http.Request, contentPath Path) string {
	/* This logic enables:
	 * - creation of HTML links that trigger "Save As.." dialog instead of being rendered by the browser
	 * - overriding the filename used when saving subresource assets on HTML page
	 * - providing a default filename for HTTP clients when downloading direct /ipfs/CID without any subpath
	 */

	// URL param ?filename=cat.jpg triggers Content-Disposition: [..] filename
	// which impacts default name used in "Save As.." dialog
	name := getFilename(contentPath)
	urlFilename := r.URL.Query().Get("filename")
	if urlFilename != "" {
		disposition := "inline"
		// URL param ?download=true triggers Content-Disposition: [..] attachment
		// which skips rendering and forces "Save As.." dialog in browsers
		if r.URL.Query().Get("download") == "true" {
			disposition = "attachment"
		}
		setContentDispositionHeader(w, urlFilename, disposition)
		name = urlFilename
	}
	return name
}

// Set Content-Disposition to arbitrary filename and disposition
func setContentDispositionHeader(w http.ResponseWriter, filename string, disposition string) {
	utf8Name := url.PathEscape(filename)
	asciiName := url.PathEscape(onlyAscii.ReplaceAllLiteralString(filename, "_"))
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=\"%s\"; filename*=UTF-8''%s", disposition, asciiName, utf8Name))
}

// Set X-Ipfs-Roots with logical CID array for efficient HTTP cache invalidation.
func (i *gatewayHandler) buildIpfsRootsHeader(contentPath string, r *http.Request) (string, error) {
	/*
		These are logical roots where each CID represent one path segment
		and resolves to either a directory or the root block of a file.
		The main purpose of this header is allow HTTP caches to do smarter decisions
		around cache invalidation (eg. keep specific subdirectory/file if it did not change)
		A good example is Wikipedia, which is HAMT-sharded, but we only care about
		logical roots that represent each segment of the human-readable content
		path:
		Given contentPath = /ipns/en.wikipedia-on-ipfs.org/wiki/Block_of_Wikipedia_in_Turkey
		rootCidList is a generated by doing `ipfs resolve -r` on each sub path:
			/ipns/en.wikipedia-on-ipfs.org → bafybeiaysi4s6lnjev27ln5icwm6tueaw2vdykrtjkwiphwekaywqhcjze
			/ipns/en.wikipedia-on-ipfs.org/wiki/ → bafybeihn2f7lhumh4grizksi2fl233cyszqadkn424ptjajfenykpsaiw4
			/ipns/en.wikipedia-on-ipfs.org/wiki/Block_of_Wikipedia_in_Turkey → bafkreibn6euazfvoghepcm4efzqx5l3hieof2frhp254hio5y7n3hv5rma
		The result is an ordered array of values:
			X-Ipfs-Roots: bafybeiaysi4s6lnjev27ln5icwm6tueaw2vdykrtjkwiphwekaywqhcjze,bafybeihn2f7lhumh4grizksi2fl233cyszqadkn424ptjajfenykpsaiw4,bafkreibn6euazfvoghepcm4efzqx5l3hieof2frhp254hio5y7n3hv5rma
		Note that while the top one will change every time any article is changed,
		the last root (responsible for specific article) may not change at all.
	*/
	var sp strings.Builder
	var pathRoots []string
	pathSegments := strings.Split(contentPath[6:], "/")
	sp.WriteString(contentPath[:5]) // /ipfs or /ipns
	for _, root := range pathSegments {
		if root == "" {
			continue
		}
		sp.WriteString("/")
		sp.WriteString(root)
		resolvedSubPath, err := ResolvePath(r.Context(), i.api, NewPath(sp.String()))
		if err != nil {
			return "", err
		}
		pathRoots = append(pathRoots, resolvedSubPath.Cid().String())
	}
	rootCidList := strings.Join(pathRoots, ",") // convention from rfc2616#sec4.2
	return rootCidList, nil
}

func webRequestError(w http.ResponseWriter, err *requestError) {
	webError(w, err.Message, err.Err, err.StatusCode)
}

func webError(w http.ResponseWriter, message string, err error, defaultCode int) {
	if _, ok := err.(resolver.ErrNoLink); ok {
		webErrorWithCode(w, message, err, http.StatusNotFound)
	} else if err == context.DeadlineExceeded {
		webErrorWithCode(w, message, err, http.StatusRequestTimeout)
	} else {
		webErrorWithCode(w, message, err, defaultCode)
	}
}

func webErrorWithCode(w http.ResponseWriter, message string, err error, code int) {
	http.Error(w, fmt.Sprintf("%s: %s", message, err), code)
	if code >= 500 {
		log.Warnf("server error: %s: %s", message, err)
	}
}

// return a 500 error and log
func internalWebError(w http.ResponseWriter, err error) {
	webErrorWithCode(w, "internalWebError", err, http.StatusInternalServerError)
}

func getFilename(contentPath Path) string {
	s := contentPath.String()
	if (strings.HasPrefix(s, ipfsPathPrefix) || strings.HasPrefix(s, ipnsPathPrefix)) && strings.Count(gopath.Clean(s), "/") <= 2 {
		// Don't want to treat ipfs.io in /ipns/ipfs.io as a filename.
		return ""
	}
	return gopath.Base(s)
}

// etagMatch evaluates if we can respond with HTTP 304 Not Modified
// It supports multiple weak and strong etags passed in If-None-Matc stringh
// including the wildcard one.
func etagMatch(ifNoneMatchHeader string, cidEtag string, dirEtag string) bool {
	buf := ifNoneMatchHeader
	for {
		buf = textproto.TrimString(buf)
		if len(buf) == 0 {
			break
		}
		if buf[0] == ',' {
			buf = buf[1:]
			continue
		}
		// If-None-Match: * should match against any etag
		if buf[0] == '*' {
			return true
		}
		etag, remain := scanETag(buf)
		if etag == "" {
			break
		}
		// Check for match both strong and weak etags
		if etagWeakMatch(etag, cidEtag) || etagWeakMatch(etag, dirEtag) {
			return true
		}
		buf = remain
	}
	return false
}

// scanETag determines if a syntactically valid ETag is present at s. If so,
// the ETag and remaining text after consuming ETag is returned. Otherwise,
// it returns "", "".
// (This is the same logic as one executed inside of http.ServeContent)
func scanETag(s string) (etag string, remain string) {
	s = textproto.TrimString(s)
	start := 0
	if strings.HasPrefix(s, "W/") {
		start = 2
	}
	if len(s[start:]) < 2 || s[start] != '"' {
		return "", ""
	}
	// ETag is either W/"text" or "text".
	// See RFC 7232 2.3.
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		switch {
		// Character values allowed in ETags.
		case c == 0x21 || c >= 0x23 && c <= 0x7E || c >= 0x80:
		case c == '"':
			return s[:i+1], s[i+1:]
		default:
			return "", ""
		}
	}
	return "", ""
}

// etagWeakMatch reports whether a and b match using weak ETag comparison.
func etagWeakMatch(a, b string) bool {
	return strings.TrimPrefix(a, "W/") == strings.TrimPrefix(b, "W/")
}

// generate Etag value based on HTTP request and CID
func getEtag(r *http.Request, cid cid.Cid) string {
	prefix := `"`
	suffix := `"`
	responseFormat, _, err := customResponseFormat(r)
	if err == nil && responseFormat != "" {
		// application/vnd.ipld.foo → foo
		f := responseFormat[strings.LastIndex(responseFormat, ".")+1:]
		// Etag: "cid.foo" (gives us nice compression together with Content-Disposition in block (raw) and car responses)
		suffix = `.` + f + suffix
	}
	// TODO: include selector suffix when https://github.com/ipfs/go-ipfs/issues/8769 lands
	return prefix + cid.String() + suffix
}

// return explicit response format if specified in request as query parameter or via Accept HTTP header
func customResponseFormat(r *http.Request) (mediaType string, params map[string]string, err error) {
	if formatParam := r.URL.Query().Get("format"); formatParam != "" {
		// translate query param to a content type
		switch formatParam {
		case "raw":
			return "application/vnd.ipld.raw", nil, nil
		case "car":
			return "application/vnd.ipld.car", nil, nil
		}
	}
	// Browsers and other user agents will send Accept header with generic types like:
	// Accept:text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8
	// We only care about explciit, vendor-specific content-types.
	for _, accept := range r.Header.Values("Accept") {
		// respond to the very first ipld content type
		if strings.HasPrefix(accept, "application/vnd.ipld") {
			mediatype, params, err := mime.ParseMediaType(accept)
			if err != nil {
				return "", nil, err
			}
			return mediatype, params, nil
		}
	}
	return "", nil, nil
}

func (i *gatewayHandler) searchUpTreeFor404(r *http.Request, contentPath Path) (Resolved, string, error) {
	filename404, ctype, err := preferred404Filename(r.Header.Values("Accept"))
	if err != nil {
		return nil, "", err
	}

	pathComponents := strings.Split(contentPath.String(), "/")

	for idx := len(pathComponents); idx >= 3; idx-- {
		pretty404 := gopath.Join(append(pathComponents[0:idx], filename404)...)
		parsed404Path := NewPath("/" + pretty404)
		if parsed404Path.IsValid() != nil {
			break
		}

		resolvedPath, err := ResolvePath(r.Context(), i.api, parsed404Path)
		if err != nil {
			continue
		}
		return resolvedPath, ctype, nil
	}

	return nil, "", fmt.Errorf("no pretty 404 in any parent folder")
}

func preferred404Filename(acceptHeaders []string) (string, string, error) {
	// If we ever want to offer a 404 file for a different content type
	// then this function will need to parse q weightings, but for now
	// the presence of anything matching HTML is enough.
	for _, acceptHeader := range acceptHeaders {
		accepted := strings.Split(acceptHeader, ",")
		for _, spec := range accepted {
			contentType := strings.SplitN(spec, ";", 1)[0]
			switch contentType {
			case "*/*", "text/*", "text/html":
				return "ipfs-404.html", "text/html", nil
			}
		}
	}

	return "", "", fmt.Errorf("there is no 404 file for the requested content types")
}

// returns unquoted path with all special characters revealed as \u codes
func debugStr(path string) string {
	q := fmt.Sprintf("%+q", path)
	if len(q) >= 3 {
		q = q[1 : len(q)-1]
	}
	return q
}

func handleUnsupportedHeaders(r *http.Request) (err *requestError) {
	// X-Ipfs-Gateway-Prefix was removed (https://github.com/ipfs/go-ipfs/issues/7702)
	// TODO: remove this after  go-ipfs 0.13 ships
	if prfx := r.Header.Get("X-Ipfs-Gateway-Prefix"); prfx != "" {
		err := fmt.Errorf("X-Ipfs-Gateway-Prefix support was removed: https://github.com/ipfs/go-ipfs/issues/7702")
		return newRequestError("unsupported HTTP header", err, http.StatusBadRequest)
	}
	return nil
}

// ?uri query param support for requests produced by web browsers
// via navigator.registerProtocolHandler Web API
// https://developer.mozilla.org/en-US/docs/Web/API/Navigator/registerProtocolHandler
// TLDR: redirect /ipfs/?uri=ipfs%3A%2F%2Fcid%3Fquery%3Dval to /ipfs/cid?query=val
func handleProtocolHandlerRedirect(w http.ResponseWriter, r *http.Request, logger *zap.SugaredLogger) (requestHandled bool) {
	if uriParam := r.URL.Query().Get("uri"); uriParam != "" {
		u, err := url.Parse(uriParam)
		if err != nil {
			webError(w, "failed to parse uri query parameter", err, http.StatusBadRequest)
			return true
		}
		if u.Scheme != "ipfs" && u.Scheme != "ipns" {
			webError(w, "uri query parameter scheme must be ipfs or ipns", err, http.StatusBadRequest)
			return true
		}
		path := u.Path
		if u.RawQuery != "" { // preserve query if present
			path = path + "?" + u.RawQuery
		}

		redirectURL := gopath.Join("/", u.Scheme, u.Host, path)
		logger.Debugw("uri param, redirect", "to", redirectURL, "status", http.StatusMovedPermanently)
		http.Redirect(w, r, redirectURL, http.StatusMovedPermanently)
		return true
	}

	return false
}

// Disallow Service Worker registration on namespace roots
// https://github.com/ipfs/go-ipfs/issues/4025
func handleServiceWorkerRegistration(r *http.Request) (err *requestError) {
	if r.Header.Get("Service-Worker") == "script" {
		matched, _ := regexp.MatchString(`^/ip[fn]s/[^/]+$`, r.URL.Path)
		if matched {
			err := fmt.Errorf("registration is not allowed for this scope")
			return newRequestError("navigator.serviceWorker", err, http.StatusBadRequest)
		}
	}

	return nil
}

// Attempt to fix redundant /ipfs/ namespace as long as resulting
// 'intended' path is valid.  This is in case gremlins were tickled
// wrong way and user ended up at /ipfs/ipfs/{cid} or /ipfs/ipns/{id}
// like in bafybeien3m7mdn6imm425vc2s22erzyhbvk5n3ofzgikkhmdkh5cuqbpbq :^))
func handleSuperfluousNamespace(w http.ResponseWriter, r *http.Request, contentPath Path) (requestHandled bool) {
	// If the path is valid, there's nothing to do
	if pathErr := contentPath.IsValid(); pathErr == nil {
		return false
	}

	// If there's no superflous namespace, there's nothing to do
	if !(strings.HasPrefix(r.URL.Path, "/ipfs/ipfs/") || strings.HasPrefix(r.URL.Path, "/ipfs/ipns/")) {
		return false
	}

	// Attempt to fix the superflous namespace
	intendedPath := NewPath(strings.TrimPrefix(r.URL.Path, "/ipfs"))
	if err := intendedPath.IsValid(); err != nil {
		webError(w, "invalid ipfs path", err, http.StatusBadRequest)
		return true
	}
	intendedURL := intendedPath.String()
	if r.URL.RawQuery != "" {
		// we render HTML, so ensure query entries are properly escaped
		q, _ := url.ParseQuery(r.URL.RawQuery)
		intendedURL = intendedURL + "?" + q.Encode()
	}
	// return HTTP 400 (Bad Request) with HTML error page that:
	// - points at correct canonical path via <link> header
	// - displays human-readable error
	// - redirects to intendedURL after a short delay

	w.WriteHeader(http.StatusBadRequest)
	if err := redirectTemplate.Execute(w, redirectTemplateData{
		RedirectURL:   intendedURL,
		SuggestedPath: intendedPath.String(),
		ErrorMsg:      fmt.Sprintf("invalid path: %q should be %q", r.URL.Path, intendedPath.String()),
	}); err != nil {
		webError(w, "failed to redirect when fixing superfluous namespace", err, http.StatusBadRequest)
	}

	return true
}

func (i *gatewayHandler) handleGettingFirstBlock(r *http.Request, begin time.Time, contentPath Path, resolvedPath Resolved) *requestError {
	// Update the global metric of the time it takes to read the final root block of the requested resource
	// NOTE: for legacy reasons this happens before we go into content-type specific code paths
	ls := i.api.NewSession(r.Context())
	f := i.api.FetcherForSession(ls)
	if _, err := f.BlockOfType(r.Context(), cidlink.Link{Cid: resolvedPath.Cid()}, basicnode.Prototype.Any); err != nil {
		return newRequestError("ipfs block get "+resolvedPath.Cid().String(), err, http.StatusInternalServerError)
	}

	ns := contentPath.Namespace()
	timeToGetFirstContentBlock := time.Since(begin).Seconds()
	i.unixfsGetMetric.WithLabelValues(ns).Observe(timeToGetFirstContentBlock) // deprecated, use firstContentBlockGetMetric instead
	i.firstContentBlockGetMetric.WithLabelValues(ns).Observe(timeToGetFirstContentBlock)
	return nil
}

func (i *gatewayHandler) setCommonHeaders(w http.ResponseWriter, r *http.Request, contentPath Path) *requestError {
	i.addUserHeaders(w) // ok, _now_ write user's headers.
	w.Header().Set("X-Ipfs-Path", contentPath.String())

	if rootCids, err := i.buildIpfsRootsHeader(contentPath.String(), r); err == nil {
		w.Header().Set("X-Ipfs-Roots", rootCids)
	} else { // this should never happen, as we resolved the contentPath already
		return newRequestError("error while resolving X-Ipfs-Roots", err, http.StatusInternalServerError)
	}

	return nil
}
