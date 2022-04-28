package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ipfs-shipyard/w3rc"
	"github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
)

// Serve content over HTTP
func Serve(c *cli.Context) error {
	if c.Bool("verbose") {
		log.SetLogLevel("*", "debug")
	}
	opts := []w3rc.Option{}
	if c.IsSet("indexer") {
		opts = append(opts, w3rc.WithIndexer(c.String("indexer")))
	}

	signalChan := make(chan os.Signal, 1)

	signal.Notify(
		signalChan,
		syscall.SIGHUP,  // kill -SIGHUP XXXX
		syscall.SIGINT,  // kill -SIGINT XXXX or Ctrl+c
		syscall.SIGQUIT, // kill -SIGQUIT XXXX
	)

	listener, err := net.Listen("tcp", c.String("listen"))
	if err != nil {
		return err
	}
	server := http.Server{
		Addr: listener.Addr().String(),
	}
	mux := http.ServeMux{}
	mux.HandleFunc("/", serve)
	server.Handler = &mux
	go server.Serve(listener)

	<-signalChan
	cctx, cancel := context.WithTimeout(c.Context, 5*time.Second)
	defer cancel()
	return server.Shutdown(cctx)
}

func serve(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello, World!"))
	return
}
