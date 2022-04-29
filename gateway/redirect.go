package gateway

import (
	"net"
	"net/http"
)

func RedirectOption(path string, redirect string) ServeOption {
	handler := &redirectHandler{redirect}
	return func(a API, _ *GatewayConfig, _ net.Listener, mux *http.ServeMux) (*http.ServeMux, error) {
		if len(path) > 0 {
			mux.Handle("/"+path+"/", handler)
		} else {
			mux.Handle("/", handler)
		}
		return mux, nil
	}
}

type redirectHandler struct {
	path string
}

func (i *redirectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, i.path, http.StatusFound)
}
