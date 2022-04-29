package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ipfs-shipyard/w3rc"
	"github.com/ipfs-shipyard/w3rc/api"
	"github.com/ipfs-shipyard/w3rc/gateway"
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

	w3rcAPI := api.NewAPI(10_000_000, opts...)

	listener, err := net.Listen("tcp", c.String("listen"))
	if err != nil {
		return err
	}
	defer listener.Close()

	gatewayConf := gateway.GatewayConfig{
		NoDNSLink: true,
		PublicGateways: map[string]*gateway.GatewaySpec{"ipfs.localhost": &gateway.GatewaySpec{
			UseSubdomains: true,
			NoDNSLink:     true,
		}},
	}

	server := gateway.Serve(w3rcAPI, &gatewayConf, listener,
		gateway.HostnameOption(),
		gateway.LogOption(),
		gateway.MetricsCollectionOption("api"),
		gateway.MetricsOpenCensusCollectionOption(),
		gateway.VersionOption(),
		gateway.WebUIOption,
		gateway.GatewayOption("/ipfs"),
	)

	<-signalChan
	cctx, cancel := context.WithTimeout(c.Context, 30*time.Second)
	defer cancel()
	return server.Shutdown(cctx)
}
