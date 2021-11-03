package main

import (
	"time"

	"github.com/urfave/cli/v2"
)

var (
	indexer     string
	indexerFlag = &cli.StringFlag{
		Name:        "indexer",
		Aliases:     []string{"i"},
		Usage:       "The indexer HTTP endpoint URL.",
		EnvVars:     []string{"W3RC_INDEXER"},
		Required:    true,
		Destination: &indexer,
	}
)

var (
	timeout     time.Duration
	timeoutFlag = &cli.DurationFlag{
		Name:        "timeout",
		Aliases:     []string{"t"},
		Usage:       "The maximum duration to wait for the command to complete.",
		EnvVars:     []string{"W3RC_TIMEOUT"},
		Required:    false,
		Hidden:      false,
		Value:       30 * time.Second,
		Destination: &timeout,
	}
)
