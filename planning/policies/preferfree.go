package policies

import "github.com/ipfs-shipyard/w3rc/planning"

type PreferFree struct{}

func (fp PreferFree) Name() planning.PolicyName { return "prefer_free" }

var _ planning.Policy = PreferFree{}
