package filecoinretrieval

import (
	"context"
	"fmt"
	"math/rand"
	"sync"

	"github.com/filecoin-project/go-address"
	datatransfer "github.com/filecoin-project/go-data-transfer"
	"github.com/filecoin-project/go-fil-markets/retrievalmarket"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/index-provider/metadata"
	"github.com/filecoin-project/specs-actors/actors/builtin/paych"
	stiapi "github.com/filecoin-project/storetheindex/api/v0"
	"github.com/ipfs-shipyard/w3rc/exchange"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/multiformats/go-multicodec"
)

var log = logging.Logger("filecoin_retrieval")

type transfer struct {
	ctx          context.Context //lint:ignore U1000 implementation in progress
	proposal     retrievalmarket.DealProposal
	pchRequired  bool
	pchAddr      address.Address
	pchLane      uint64
	nonce        uint64
	totalPayment abi.TokenAmount //lint:ignore U1000 implementation in progress
	events       chan exchange.EventData
}

type Event datatransfer.Event
type State datatransfer.ChannelState

type VoucherCreateResult struct {
	// Voucher that was created, or nil if there was an error or if there
	// were insufficient funds in the channel
	Voucher *paych.SignedVoucher
	// Shortfall is the additional amount that would be needed in the channel
	// in order to be able to create the voucher
	Shortfall big.Int
}

type PaymentAPI interface {
	GetPaychWithMinFunds(ctx context.Context, dest address.Address) (address.Address, error)
	AllocateLane(ctx context.Context, payCh address.Address) (uint64, error)
	CreateVoucher(ctx context.Context, payCh address.Address, vouch paych.SignedVoucher) (*VoucherCreateResult, error)
}

type FilecoinExchange struct {
	paymentAPI   PaymentAPI
	dataTransfer datatransfer.Manager
	host         host.Host
	transfers    map[datatransfer.ChannelID]*transfer
	transfersLk  sync.RWMutex
}

func NewFilecoinExchange(node PaymentAPI, h host.Host, dataTransfer datatransfer.Manager) *FilecoinExchange {
	return &FilecoinExchange{
		paymentAPI:   node,
		host:         h,
		dataTransfer: dataTransfer,
		transfers:    make(map[datatransfer.ChannelID]*transfer),
	}
}

//lint:ignore U1000 implementation in progress
func finishWithError(tf *transfer, err error) {
	tf.events <- exchange.EventData{Event: exchange.FailureEvent, State: err}
	close(tf.events)
}

//lint:ignore U1000 implementation in progress
func (fe *FilecoinExchange) subscriber(event datatransfer.Event, channelState datatransfer.ChannelState) {
	// Copy chanid so it can be used later in the callback
	fe.transfersLk.RLock()
	tf, ok := fe.transfers[channelState.ChannelID()]
	if !ok {
		fe.transfersLk.RUnlock()
		return
	}
	fe.transfersLk.RUnlock()

	// TODO: are these the correct mappings?
	ev := exchange.ProgressEvent
	switch event.Code {
	case datatransfer.FinishTransfer:
	case datatransfer.Complete:
		ev = exchange.SuccessEvent
	case datatransfer.Error:
	case datatransfer.Disconnected:
		ev = exchange.ErrorEvent
	case datatransfer.ReceiveDataError:
		ev = exchange.FailureEvent
	}

	select {
	case <-tf.ctx.Done():
		finishWithError(tf, tf.ctx.Err())
		return
	case tf.events <- exchange.EventData{Event: ev, State: channelState}:
	}

	switch event.Code {
	case datatransfer.NewVoucherResult:
		switch resType := channelState.LastVoucherResult().(type) {
		case *retrievalmarket.DealResponse:
			switch resType.Status {
			case retrievalmarket.DealStatusAccepted:
				log.Info("deal accepted")

			// Respond with a payment voucher when funds are requested
			case retrievalmarket.DealStatusFundsNeeded:
				if tf.pchRequired {
					log.Infof("sending payment voucher (nonce: %v, amount: %v)", tf.nonce, resType.PaymentOwed)

					tf.totalPayment = big.Add(tf.totalPayment, resType.PaymentOwed)

					vres, err := fe.paymentAPI.CreateVoucher(tf.ctx, tf.pchAddr, paych.SignedVoucher{
						ChannelAddr: tf.pchAddr,
						Lane:        tf.pchLane,
						Nonce:       tf.nonce,
						Amount:      tf.totalPayment,
					})
					if err != nil {
						finishWithError(tf, err)
						return
					}

					if big.Cmp(vres.Shortfall, big.NewInt(0)) > 0 {
						finishWithError(tf, fmt.Errorf("not enough funds remaining in payment channel (shortfall = %s)", vres.Shortfall))
						return
					}

					if err := fe.dataTransfer.SendVoucher(tf.ctx, channelState.ChannelID(), &retrievalmarket.DealPayment{
						ID:             tf.proposal.ID,
						PaymentChannel: tf.pchAddr,
						PaymentVoucher: vres.Voucher,
					}); err != nil {
						finishWithError(tf, fmt.Errorf("failed to send payment voucher: %w", err))
						return
					}

					tf.nonce++
				} else {
					finishWithError(tf, fmt.Errorf("the miner requested payment even though this transaction was determined to be zero cost"))
				}
			case retrievalmarket.DealStatusFundsNeededUnseal:
				finishWithError(tf, fmt.Errorf("received unexpected payment request for unsealing data"))
			default:
				log.Debugf("unrecognized voucher response status: %v", retrievalmarket.DealStatuses[resType.Status])
			}
		default:
			log.Debugf("unrecognized voucher response type: %v", resType)
		}
	case datatransfer.DataReceived:
		// Ignore this
	case datatransfer.FinishTransfer:
		close(tf.events)
	case datatransfer.Cancel:
		finishWithError(tf, fmt.Errorf("data transfer canceled"))
	default:
		log.Debugf("unrecognized data transfer event: %v", datatransfer.Events[event.Code])
	}
}

func (fe *FilecoinExchange) Code() multicodec.Code {
	return multicodec.Code(4128768)
}

func singleTerminalError(err error) <-chan exchange.EventData {
	resultChan := make(chan exchange.EventData)
	resultChan <- exchange.EventData{Event: exchange.FailureEvent, State: err}
	close(resultChan)
	return resultChan
}

func (fe *FilecoinExchange) RequestData(ctx context.Context, root ipld.Link, selector ipld.Node, routingProvider interface{}, routingPayload interface{}) <-chan exchange.EventData {

	var tf transfer

	ai, ok := routingProvider.(peer.AddrInfo)
	if !ok {
		return singleTerminalError(fmt.Errorf("routing provider is not in expected format"))
	}

	fe.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.TempAddrTTL)
	miner := ai.ID

	dtm, err := metadata.FromIndexerMetadata(stiapi.Metadata{
		ProtocolID: fe.Code(),
		Data:       routingPayload.([]byte),
	})
	if err != nil {
		return singleTerminalError(err)
	}
	filData, err := metadata.DecodeFilecoinV1Data(dtm)
	if err != nil {
		return singleTerminalError(err)
	}
	if !filData.FastRetrieval && !filData.VerifiedDeal {
		return singleTerminalError(fmt.Errorf("err not implemented"))
	}

	params, err := retrievalmarket.NewParamsV1(
		big.NewInt(0),
		0,
		0,
		selector,
		&filData.PieceCID,
		big.NewInt(0),
	)

	if err != nil {
		return singleTerminalError(err)
	}

	tf.proposal = retrievalmarket.DealProposal{
		PayloadCID: root.(cidlink.Link).Cid,
		ID:         retrievalmarket.DealID(rand.Int63n(1000000) + 100000),
		Params:     params,
	}

	log.Infof("starting retrieval with miner: %s", miner)

	// Stats

	tf.pchRequired = !(tf.proposal.PricePerByte.Int != nil && tf.proposal.PricePerByte.IsZero()) || !(tf.proposal.UnsealPrice.Int != nil && tf.proposal.UnsealPrice.IsZero())

	if tf.pchRequired {
		// Get the payment channel and create a lane for this retrieval
		tf.pchAddr, err = fe.paymentAPI.GetPaychWithMinFunds(ctx, address.Address{})
		if err != nil {
			return singleTerminalError(fmt.Errorf("failed to get payment channel: %w", err))
		}
		tf.pchLane, err = fe.paymentAPI.AllocateLane(ctx, tf.pchAddr)
		if err != nil {
			return singleTerminalError(fmt.Errorf("failed to allocate lane: %w", err))
		}
	}

	fe.transfersLk.Lock()
	defer fe.transfersLk.Unlock()
	chid, err := fe.dataTransfer.OpenPullDataChannel(ctx, miner, &tf.proposal, tf.proposal.PayloadCID, selector)
	if err != nil {
		return singleTerminalError(err)
	}
	tf.events = make(chan exchange.EventData)
	fe.transfers[chid] = &tf
	return tf.events
}
