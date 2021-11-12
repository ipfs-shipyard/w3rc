package planning

import "github.com/ipfs-shipyard/w3rc/contentrouting"

type Policy interface{}

type PolicyScorer interface {
	// must be in range from zero to 1, will get dropped otherwise
	// should return zero for policies that are unrecognized
	Score(Policy) PolicyScore
}

type PolicyWeight float64
type PolicyPreferences struct {
	preferences map[PolicyWeight]Policy
}

type PolicyScore float64

func (p *PolicyPreferences) WeightedScore(scorer PolicyScorer, transportMultipler PolicyWeight) PolicyScore {
	score := PolicyScore(0)
	for weight, policy := range p.preferences {
		pscore := scorer.Score(policy)
		if pscore < 0 || pscore > 1 {
			continue
		}
		score += pscore * PolicyScore(weight)
	}
	score *= PolicyScore(transportMultipler)
	return score
}

func (p *PolicyPreferences) AddPolicy(weight PolicyWeight, policy Policy) {
	p.preferences[weight] = policy
}

func (p *PolicyPreferences) Policies() []Policy {
	policies := make([]Policy, 0, len(p.preferences))
	for _, policy := range p.preferences {
		policies = append(policies, policy)
	}
	return policies
}

// RoutingRecordInterpreter interprets records for a given multicodec range
type RoutingRecordInterpreter interface {
	Interpret(record contentrouting.RoutingRecord) (PolicyScorer, error)
}

type PotentialRequest struct {
	PolicyScore
	contentrouting.RoutingRecord
}
