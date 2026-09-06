package policy

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/score"
)

// Reader answers what is in force. It is a type rather than a set of functions
// so that a gate can hold one behind an interface and a test can hold a fake.
type Reader struct {
	pool *pgxpool.Pool
	// token fences the read events the log appends, and is what
	// [Reader.ScoreVersionAtGate] passes to package score to read which version
	// is in force at one scope.
	token lease.Token
	// log is how every policy version is read. A version is a row of the
	// decision log, so reading one is reading the log, which appends a read
	// event of its own.
	log *decisionlog.Reader
	// score is the score version the supplied half of every answer is read out
	// of. It is held rather than read per answer because a supplied value moves
	// as outcomes arrive: a reader that read the newest version at each resolve
	// could give one gate firing a threshold from one version and a decision row
	// naming another, and the row would not be readable against the policy it was
	// decided under.
	score score.Version
}

// NewReader returns the reader over pool, reading the log's rows with token and
// reading what the score supplies out of version. The zero version is the
// starting values — the numbers the formula was calibrated at — which is what a
// factory that has appended no version yet supplies, so a reader composed
// before the first ensure answers with those and not with nothing.
func NewReader(pool *pgxpool.Pool, token lease.Token, version score.Version) *Reader {
	return &Reader{pool: pool, token: token, log: decisionlog.NewReader(pool, token), score: version}
}

// Subjects is what a read is performed against: the records whose fields hold
// each parameter, and through them the subjects a safeguard may be drawn on. A
// field left empty is a record the caller has none of, and the parameters scoped
// to it resolve to what the score supplies with no authored value to find.
type Subjects struct {
	GateRow       string
	EnvironmentID string
	ServiceID     string
	// ProjectID is the project a safeguard drawn on a project reaches this
	// read through. It is a subject of its own, the design's list naming a
	// project beside a service and an area.
	ProjectID string
	// AreaID is the narrowest area; the chain above it is walked, because a
	// safeguard drawn on any area in the chain reaches an item in the narrowest.
	AreaID string
	Stage  item.Stage
	// Quantity is which quantity a per-quantity parameter is read for: the
	// analysis window's size and its power are one value per quantity, and a
	// read that names none finds nothing authored, because a value authored for
	// one quantity is not a value for another.
	Quantity string
	// Duty is which of the owner's twelve duties a per-duty parameter is read
	// for: the review sample rate is one value per duty, and a read that names
	// none — the zero value, no duty being numbered zero — finds nothing
	// authored.
	Duty int
}
