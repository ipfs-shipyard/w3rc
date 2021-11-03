package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
)

func main() {
	os.Exit(run())
}

func run() int {
	app := &cli.App{
		Name:  "w3rc",
		Usage: "An experimental retrieval client for web3 content",
		Commands: []*cli.Command{
			findCommand,
		},
	}
	if err := app.Run(os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
