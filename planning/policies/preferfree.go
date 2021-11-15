package policies

import "github.com/ipfs-shipyard/w3rc/planning"

type PreferFree struct{}

var _ planning.Policy = PreferFree{}
