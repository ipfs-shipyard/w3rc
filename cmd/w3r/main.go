package main

import (
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

func main() { os.Exit(main1()) }

func main1() int {
	app := &cli.App{
		Name:  "w3r",
		Usage: "Web3 Retrieval",
		Commands: []*cli.Command{
			{
				Name:    "get",
				Usage:   "Get a CID",
				Aliases: []string{"g"},
				Action:  Get,
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "recursive",
						Aliases: []string{"r"},
						Usage:   "Get the dag pointed to by the CID recursively",
					},
					&cli.StringFlag{
						Name:    "file",
						Aliases: []string{"f", "o", "output"},
						Usage:   "output to a file rather than stdout",
					},
					&cli.StringFlag{
						Name:  "indexer",
						Usage: "query a specific indexer endpoint",
					},
					&cli.BoolFlag{
						Name:    "verbose",
						Aliases: []string{"v"},
						Usage:   "verbose output",
					},
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Println(err)
		return 1
	}
	return 0
}
