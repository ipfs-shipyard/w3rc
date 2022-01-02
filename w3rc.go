package w3rc

import (
	"context"

	"github.com/ipfs-shipyard/w3rc/contentrouting/delegated"
	"github.com/ipfs-shipyard/w3rc/exchange"
	"github.com/ipfs-shipyard/w3rc/exchange/filecoinretrieval"
	"github.com/ipfs-shipyard/w3rc/planning"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
)

var log = logging.Logger("w3rc")

// NewSession creates a Session with given configuration.
// A session represents a set of related queries for content addressed data.
// Connections to peers may stay open for the life of a session.
func NewSession(ls ipld.LinkSystem, opts ...Option) (Session, error) {
	conf := config{}
	if err := apply(&conf, opts...); err != nil {
		return nil, err
	}
	if err := applyDefaults(ls, &conf); err != nil {
		return nil, err
	}
	router, err := delegated.NewDelegatedHTTP(conf.indexerURL)
	if err != nil {
		return nil, err
	}

	session := simpleSession{
		ls:        ls,
		router:    router,
		mux:       exchange.DefaultMux(),
		scheduler: planning.NewSimpleScheduler(),
	}

	dt := conf.dt
	fce := filecoinretrieval.NewFilecoinExchange(nil, conf.host, dt)
	session.mux.Register(fce)

	return &session, nil
}

// ResultChan provides progress updates from a call to `GetStream`
type ResultChan chan ProgressResult

// A ProgressResult is an individual update from a call to `GetStream`
// The result will either have a status of `Error` and an Error set,
// or will have a node and path set.
// The ResultChan a result is sent down will close after an error result
// or 'complete' result is sent.
type ProgressResult struct {
	Status
	Error error
	ipld.Path
	ipld.Node
}

// Status is a code of the type of an individual Progress Result
type Status uint8

// These are valid status codes
const (
	ERROR Status = iota
	INPROGRESS
	COMPLETE
)

// A Session is able to fetch content addressed data.
type Session interface {
	// Get returns a dag rooted at root. If selector is `nil`, the single block
	// of the root will be assumed. If the full dag under root is desired, (following links)
	// `CommonSelector_MatchAllRecursively` should be provided.
	Get(ctx context.Context, root cid.Cid, selector datamodel.Node) (ipld.Node, error)

	// TODO:
	//GetStream(ctx context.Context, root cid.Cid, selector datamodel.Node) ResultChan
	Close() error
}
