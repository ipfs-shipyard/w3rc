package filecoinretrieval

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"sync"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	datatransfer "github.com/filecoin-project/go-data-transfer"
	"github.com/filecoin-project/go-fil-markets/retrievalmarket"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/specs-actors/actors/builtin/paych"
	"github.com/ipfs-shipyard/w3rc/exchange"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	peer "github.com/libp2p/go-libp2p-core/peer"
	"github.com/multiformats/go-multicodec"
)

var log = logging.Logger("filecoin_retrieval")

type transfer struct {
	ctx          context.Context
	proposal     retrievalmarket.DealProposal
	pchRequired  bool
	pchAddr      address.Address
	pchLane      uint64
	nonce        uint64
	totalPayment abi.TokenAmount
	events       chan exchange.EventData
	errors       chan error
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

type Node interface {
	GetPaychWithMinFunds(ctx context.Context, dest address.Address) (address.Address, error)
	AllocateLane(ctx context.Context, payCh address.Address) (uint64, error)
	CreateVoucher(ctx context.Context, payCh address.Address, vouch paych.SignedVoucher) (*VoucherCreateResult, error)
}

type FilecoinExchange struct {
	node         Node
	dataTransfer datatransfer.Manager
	transfers    map[datatransfer.ChannelID]*transfer
	transfersLk  sync.RWMutex
}

func NewFilecoinExchange(node Node, dataTransfer datatransfer.Manager) *FilecoinExchange {
	return &FilecoinExchange{
		node:         node,
		dataTransfer: dataTransfer,
	}
}

func finishWithError(tf *transfer, err error) {
	close(tf.events)
	tf.errors <- err
	close(tf.errors)
}

func (fe *FilecoinExchange) subscriber(event datatransfer.Event, channelState datatransfer.ChannelState) {
	// Copy chanid so it can be used later in the callback
	fe.transfersLk.RLock()
	tf, ok := fe.transfers[channelState.ChannelID()]
	if !ok {
		fe.transfersLk.RUnlock()
		return
	}
	fe.transfersLk.RUnlock()

	select {
	case <-tf.ctx.Done():
		finishWithError(tf, tf.ctx.Err())
		return
	case tf.events <- exchange.EventData{event, channelState}:
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

					vres, err := fe.node.CreateVoucher(tf.ctx, tf.pchAddr, paych.SignedVoucher{
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
		close(tf.errors)
	case datatransfer.Cancel:
		finishWithError(tf, fmt.Errorf("data transfer canceled"))
	default:
		log.Debugf("unrecognized data transfer event: %v", datatransfer.Events[event.Code])
	}
}

func (fe *FilecoinExchange) Code() multicodec.Code {
	// TODO: fill in
	return multicodec.Code(0)
}

func singleTerminalError(err error) (<-chan exchange.EventData, <-chan error) {
	resultChan := make(chan exchange.EventData)
	errChan := make(chan error, 1)
	errChan <- err
	close(resultChan)
	close(errChan)
	return resultChan, errChan
}

func (fe *FilecoinExchange) RequestData(ctx context.Context, root ipld.Link, selector ipld.Node, routingProvider interface{}, routingPayload interface{}) (<-chan exchange.EventData, <-chan error) {

	var tf transfer

	miner := peer.ID(routingProvider.(string))

	var resp retrievalmarket.QueryResponse
	if err := cborutil.ReadCborRPC(bytes.NewReader(routingPayload.([]byte)), &resp); err != nil {
		return singleTerminalError(err)
	}

	params, err := retrievalmarket.NewParamsV1(
		resp.MinPricePerByte,
		resp.MaxPaymentInterval,
		resp.MaxPaymentIntervalIncrease,
		selector,
		nil,
		resp.UnsealPrice,
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

	tf.pchRequired = !tf.proposal.PricePerByte.IsZero() || !tf.proposal.UnsealPrice.IsZero()

	if tf.pchRequired {
		// Get the payment channel and create a lane for this retrieval
		tf.pchAddr, err = fe.node.GetPaychWithMinFunds(ctx, resp.PaymentAddress)
		if err != nil {
			return singleTerminalError(fmt.Errorf("failed to get payment channel: %w", err))
		}
		tf.pchLane, err = fe.node.AllocateLane(ctx, tf.pchAddr)
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
	tf.errors = make(chan error, 1)
	fe.transfers[chid] = &tf
	return tf.events, tf.errors
}
