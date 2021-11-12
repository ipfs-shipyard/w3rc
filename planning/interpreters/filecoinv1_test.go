package interpreters

import (
	"errors"
	"testing"

	"github.com/filecoin-project/indexer-reference-provider/metadata"
	"github.com/ipfs-shipyard/w3rc/contentrouting"
	"github.com/ipfs-shipyard/w3rc/planning"
	"github.com/ipfs-shipyard/w3rc/planning/policies"
	"github.com/ipfs-shipyard/w3rc/testutil"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
)

var _ contentrouting.RoutingRecord = (*testRoutingRecord)(nil)

type testRoutingRecord struct {
	request  cid.Cid
	protocol multicodec.Code
	payload  interface{}
}

func (t testRoutingRecord) Request() cid.Cid {
	return t.request
}

func (t testRoutingRecord) Protocol() multicodec.Code {
	return t.protocol
}

func (t testRoutingRecord) Provider() interface{} {
	return nil
}

func (t testRoutingRecord) Payload() interface{} {
	return t.payload
}

func TestFilecoinV1RecordInterpreter_Interpret(t *testing.T) {
	tests := map[string]struct {
		givenRecord       contentrouting.RoutingRecord
		wantPolicyResults map[planning.Policy]planning.PolicyScore
		wantErr           string
	}{
		"RoutingErrorRecordIsError": {
			givenRecord: &contentrouting.RoutingError{
				Error: errors.New("fish"),
			},
			wantErr: "fish",
		},
		"InvalidRoutingErrorRecordIsError": {
			givenRecord: &testRoutingRecord{
				protocol: contentrouting.RoutingErrorProtocol,
				payload:  "not error",
			},
			wantErr: "routing record payload not match expected type: error",
		},
		"InvalidPayloadIsError": {
			givenRecord: &testRoutingRecord{
				payload: 42,
			},
			wantErr: "filecoin v1 routing record payload does not match expected type: []byte",
		},
		"NonDataTransferMulticodecIsError": {
			givenRecord: &testRoutingRecord{
				protocol: multicodec.DagCbor,
				payload:  []byte("fish"),
			},
			wantErr: "protocol 0x71 is not a data transfer protocol",
		},
		"NonFilecoinV1ExchangeFormatIsError": {
			givenRecord: &testRoutingRecord{
				protocol: metadata.DataTransferMulticodec(metadata.ExchangeFormat(42), metadata.GraphSyncV1),
				payload:  []byte("fish"),
			},
			wantErr: "protocol 0x3F2A00 does not use the FilecoinV1 exchange format",
		},
		"PaidFilecoinV1ExchangeScoreIsZeroForPreferFreePolicy": {
			givenRecord: generateFilecoinV1RoutingRecord(t, metadata.FilecoinV1Data{
				PieceCID: testutil.GenerateCids(1)[0],
			}),
			wantPolicyResults: map[planning.Policy]planning.PolicyScore{
				policies.PreferFree{}: planning.PolicyScore(0),
			},
		},
		"FreeFilecoinV1ExchangeScoreIsOneForPreferFreePolicy": {
			givenRecord: generateFilecoinV1RoutingRecord(t, metadata.FilecoinV1Data{
				PieceCID: testutil.GenerateCids(1)[0],
				IsFree:   true,
			}),
			wantPolicyResults: map[planning.Policy]planning.PolicyScore{
				policies.PreferFree{}: planning.PolicyScore(1),
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fri := &FilecoinV1RecordInterpreter{}
			got, err := fri.Interpret(tt.givenRecord)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("Interpret() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Interpret() failed to get policy results: %v", err)
			}
			if got == nil {
				t.Fatal("Interpret() policy results must not be nil")
			}
			for wantName, wantScore := range tt.wantPolicyResults {
				gotScore := got.Score(wantName)
				if wantScore != gotScore {
					t.Fatalf("Interpret() unexpected score for policy name %v: want %v, got %v", wantName, wantScore, gotScore)
				}
			}
		})
	}
}

func generateFilecoinV1RoutingRecord(t *testing.T, fv1d metadata.FilecoinV1Data) contentrouting.RoutingRecord {
	p := metadata.DataTransferMulticodec(metadata.FilecoinV1, metadata.GraphSyncV1)
	dtm, err := fv1d.Encode(metadata.GraphSyncV1)
	if err != nil {
		t.Fatalf("failed to encode FilecoinV1 data transfer metadata: %v", err)
	}
	return &testRoutingRecord{
		protocol: p,
		payload:  dtm.Data,
	}
}
