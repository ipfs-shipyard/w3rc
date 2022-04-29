package gateway

import (
	"fmt"
	"net"
	"net/http"
	"sort"

	id "github.com/libp2p/go-libp2p/p2p/protocol/identify"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// ServeOption registers any HTTP handlers it provides on the given mux.
// It returns the mux to expose to future options, which may be a new mux if it
// is interested in mediating requests to future options, or the same mux
// initially passed in if not.
type ServeOption func(API, *GatewayConfig, net.Listener, *http.ServeMux) (*http.ServeMux, error)

// A helper function to clean up a set of headers:
// 1. Canonicalizes.
// 2. Deduplicates.
// 3. Sorts.
func cleanHeaderSet(headers []string) []string {
	// Deduplicate and canonicalize.
	m := make(map[string]struct{}, len(headers))
	for _, h := range headers {
		m[http.CanonicalHeaderKey(h)] = struct{}{}
	}
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}

	// Sort
	sort.Strings(result)
	return result
}

func GatewayOption(paths ...string) ServeOption {
	return func(a API, cfg *GatewayConfig, _ net.Listener, mux *http.ServeMux) (*http.ServeMux, error) {
		headers := make(map[string][]string, len(cfg.HTTPHeaders))
		for h, v := range cfg.HTTPHeaders {
			headers[http.CanonicalHeaderKey(h)] = v
		}

		// Hard-coded headers.
		const ACAHeadersName = "Access-Control-Allow-Headers"
		const ACEHeadersName = "Access-Control-Expose-Headers"
		const ACAOriginName = "Access-Control-Allow-Origin"
		const ACAMethodsName = "Access-Control-Allow-Methods"

		if _, ok := headers[ACAOriginName]; !ok {
			// Default to *all*
			headers[ACAOriginName] = []string{"*"}
		}
		if _, ok := headers[ACAMethodsName]; !ok {
			// Default to GET
			headers[ACAMethodsName] = []string{http.MethodGet}
		}

		headers[ACAHeadersName] = cleanHeaderSet(
			append([]string{
				"Content-Type",
				"User-Agent",
				"Range",
				"X-Requested-With",
			}, headers[ACAHeadersName]...))

		headers[ACEHeadersName] = cleanHeaderSet(
			append([]string{
				"Content-Range",
				"X-Chunked-Output",
				"X-Stream-Output",
			}, headers[ACEHeadersName]...))

		var gateway http.Handler = newGatewayHandler(cfg, a)

		for _, p := range paths {
			mux.Handle(p+"/", gateway)
		}
		return mux, nil
	}
}

func VersionOption() ServeOption {
	return func(a API, cfg *GatewayConfig, _ net.Listener, mux *http.ServeMux) (*http.ServeMux, error) {
		mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
			//fmt.Fprintf(w, "Commit: %s\n", version.CurrentCommit)
			fmt.Fprintf(w, "Protocol Version: %s\n", id.LibP2PVersion)
		})
		return mux, nil
	}
}

// makeHandler turns a list of ServeOptions into a http.Handler that implements
// all of the given options, in order.
func makeHandler(a API, gc *GatewayConfig, l net.Listener, options ...ServeOption) (http.Handler, error) {
	topMux := http.NewServeMux()
	mux := topMux
	for _, option := range options {
		var err error
		mux, err = option(a, gc, l, mux)
		if err != nil {
			return nil, err
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ServeMux does not support requests with CONNECT method,
		// so we need to handle them separately
		// https://golang.org/src/net/http/request.go#L111
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusOK)
			return
		}
		topMux.ServeHTTP(w, r)
	})
	return handler, nil
}

// ListenAndServe runs an HTTP server listening at |listeningMultiAddr| with
// the given serve options. The address must be provided in multiaddr format.
//
// TODO intelligently parse address strings in other formats so long as they
// unambiguously map to a valid multiaddr. e.g. for convenience, ":8080" should
// map to "/ip4/0.0.0.0/tcp/8080".
func ListenAndServe(a API, gc *GatewayConfig, listeningMultiAddr string, options ...ServeOption) (*http.Server, error) {
	addr, err := ma.NewMultiaddr(listeningMultiAddr)
	if err != nil {
		return nil, err
	}

	list, err := manet.Listen(addr)
	if err != nil {
		return nil, err
	}

	// we might have listened to /tcp/0 - let's see what we are listing on
	addr = list.Multiaddr()
	fmt.Printf("API server listening on %s\n", addr)

	return Serve(a, gc, manet.NetListener(list), options...), nil
}

// Serve accepts incoming HTTP connections on the listener and pass them
// to ServeOption handlers.
func Serve(a API, gc *GatewayConfig, lis net.Listener, options ...ServeOption) *http.Server {
	// make sure we close this no matter what.
	//defer lis.Close()

	handler, err := makeHandler(a, gc, lis, options...)
	if err != nil {
		return nil
	}

	server := &http.Server{
		Addr:    lis.Addr().String(),
		Handler: handler,
	}

	go func() {
		//todo: pipe out error.
		err := server.Serve(lis)
		if err != nil {
			log.Warnf("Serving exited: %s", err)
		}
	}()

	return server
}
