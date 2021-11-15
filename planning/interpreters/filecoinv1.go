package interpreters

import (
	"errors"

	"github.com/filecoin-project/indexer-reference-provider/metadata"
	v0 "github.com/filecoin-project/storetheindex/api/v0"
	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs-shipyard/w3rc/planning"
	"github.com/ipfs-shipyard/w3rc/planning/policies"
)

var _ planning.RoutingRecordInterpreter = (*FilecoinV1RecordInterpreter)(nil)

type FilecoinV1RecordInterpreter struct {
}

func (fri FilecoinV1RecordInterpreter) Interpret(record contentrouting.RoutingRecord) (planning.PolicyScorer, error) {

	// decode the record (or error) -- use metadata from filecoin
	// check for free or paid policy
	// return PolicyResults that when given "prefer_free" returns 1 if retrieval is free or zero if its paid

	if record.Protocol() == contentrouting.RoutingErrorProtocol {
		err, ok := record.Payload().(error)
		if !ok {
			return nil, errors.New("routing record payload does not match expected type: error")
		}
		return nil, err
	}

	data, ok := record.Payload().([]byte)
	if !ok {
		return nil, errors.New("filecoin v1 routing record payload does not match expected type: []byte")
	}
	rm := v0.Metadata{
		ProtocolID: record.Protocol(),
		Data:       data,
	}

	dtm, err := metadata.FromIndexerMetadata(rm)
	if err != nil {
		return nil, err
	}

	fv1d, err := metadata.DecodeFilecoinV1Data(dtm)
	if err != nil {
		return nil, err
	}

	return &simplePolicyResults{isFree: fv1d.IsFree}, nil
}

var _ planning.PolicyScorer = (*simplePolicyResults)(nil)

type simplePolicyResults struct {
	isFree bool
}

func (s simplePolicyResults) Score(policy planning.Policy) planning.PolicyScore {
	switch policy.(type) {
	case policies.PreferFree:
		if s.isFree {
			return 1
		}
	}
	return 0
}
