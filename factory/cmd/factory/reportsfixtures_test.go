// The report channel as a test drives it: the report store on a schema of its
// own, the entrance served over it the way [serveCommand] serves it, and the
// deploy target wrapped so a test can read what one deploy carried across the
// seam.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/reportstore"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/wayin"
)

// reports is the channel a test drives: the store, the entrance in front of
// it, and the deployments this run handed a target.
type reports struct {
	channel *reportChannel
	// reads is a second pool over the same store, for the rows this test
	// asserts over. The store answers counts and a report by id, and a
	// submission that arrived through a deployed service's way in leaves the
	// test holding neither, so what it reads the row by is the table.
	reads *pgxpool.Pool
	// url is the entrance, which is what the deploy path hands a service as
	// the address its way in posts to.
	url string
	// list is the erasure list this store is the one writer of, for the tests
	// that read the row an erasure appended.
	list   string
	placed *placements
}

// newReports composes the report channel the way serve does — the store on a
// schema of its own, the five interfaces over the graph, the entrance served
// over it — and points the deploy path at it. It is called before run, because
// the address is handed to a service at the deploy that starts it.
func newReports(t *testing.T, ctx context.Context, d *deps) *reports {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the report store's schema: %v", err)
	}
	// A schema of its own on the same server, the way newPathIn gives the
	// graph one. The report store is a second database with no default URL,
	// and reportstore.Apply creates the schema its search path names.
	schema := "reportstore_" + hex.EncodeToString(suffix[:])
	url := inSchema(t, postgres.URL(), schema)

	// The erasure list beside the store, which is its one writer: the store
	// appends a row at every redaction and People's deletion of a mapping
	// appends through the same store, so one file holds everything erased.
	list := filepath.Join(t.TempDir(), "erasure-list")
	channel, closeStore, err := openReportStore(ctx, url, list, d.pool, d.token)
	if err != nil {
		t.Fatalf("opening the report store at %s: %v", url, err)
	}
	t.Cleanup(closeStore)

	reads, err := reportstore.Open(ctx, url)
	if err != nil {
		t.Fatalf("opening a second connection to the report store: %v", err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := reads.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		reads.Close()
	})

	// The store is on the composition for the erasure list it is the one writer
	// of, which is what People's deletion of a mapping appends its row through.
	d.reports = channel.store

	entrance := httptest.NewServer(wayin.NewEntrance(channel))
	t.Cleanup(entrance.Close)
	d.wayInAddress = entrance.URL

	// The targets, wrapped. The way-in token is minted at the deploy and
	// stored nowhere — the record holds its digest — so the seam is the one
	// place a test can read what a deployed service was given, which is what a
	// session at that service's way in presents and nothing else has.
	placed := &placements{}
	targets := newTargetSet(func(dir string) targetseam.Target {
		return &recordingTarget{Target: localtarget.New(dir), dir: dir, placed: placed}
	})
	d.targets = targets
	t.Cleanup(func() {
		for dir, target := range targets.made {
			for _, named := range d.services {
				if _, err := target.Stop(context.Background(), deployerPrincipal, named.name, d.credential); err != nil {
					t.Errorf("stopping the %s service on %s: %v", named.name, dir, err)
				}
			}
		}
	})
	return &reports{channel: channel, reads: reads, url: entrance.URL, list: list, placed: placed}
}

// placement is one deploy as the seam carried it: which service on which
// target, and the two values the way in inside it was given.
type placement struct {
	dir, service, build, token, address string
}

// placements is what a run handed the targets, in order.
type placements struct{ made []placement }

// recordingTarget is [localtarget.Local] with what crossed the seam kept. It
// embeds the target rather than delegating each operation: what this wrapper
// is about is one of the seven, and the other six are the local target's
// unchanged.
type recordingTarget struct {
	targetseam.Target
	dir    string
	placed *placements
}

func (r *recordingTarget) Deploy(ctx context.Context, p principal.Principal,
	d targetseam.Deployment) (targetseam.Placement, error) {
	r.placed.made = append(r.placed.made, placement{
		dir: r.dir, service: d.Service, build: d.Build,
		token: wayInTokenIn(d.Configuration), address: d.WayInAddress,
	})
	return r.Target.Deploy(ctx, p, d)
}

// wayInTokenIn is the token the deployer minted for a deploy, read out of the
// configuration it hands the service: the token is one of that value set's
// values, under the name the way in inside the build reads it by.
func wayInTokenIn(values targetseam.ValueSet) string {
	for n, name := range values.Names {
		if name == deploy.WayInTokenName && n < len(values.Values) {
			return values.Values[n]
		}
	}
	return ""
}

// at is the newest deploy of one service on one target, which for production's
// directory is the release that is running there now.
func (r *reports) at(t *testing.T, dir, service string) placement {
	t.Helper()
	for n := len(r.placed.made) - 1; n >= 0; n-- {
		if made := r.placed.made[n]; made.dir == dir && made.service == service {
			return made
		}
	}
	t.Fatalf("no deploy of %s reached %s; the run placed %+v", service, dir, r.placed.made)
	return placement{}
}

// stored is every report in the store, oldest first, in the fields this
// demonstration asserts over.
type stored struct {
	serviceID, environmentID, deployID string
	kind, text, identity               string
	harmMarked                         bool
	sourceKey, noticeID                string
}

func (r *reports) stored(t *testing.T, ctx context.Context) []stored {
	t.Helper()
	rows, err := r.reads.Query(ctx, `select service_id, environment_id, deploy_id, kind, text,
		shipped_bundle_identity, harm_marked, source_key, notice_id
		from `+reportstore.ReportTable+` order by collected_at, id`)
	if err != nil {
		t.Fatalf("reading the reports: %v", err)
	}
	defer rows.Close()
	var read []stored
	for rows.Next() {
		var one stored
		if err := rows.Scan(&one.serviceID, &one.environmentID, &one.deployID, &one.kind,
			&one.text, &one.identity, &one.harmMarked, &one.sourceKey, &one.noticeID); err != nil {
			t.Fatalf("reading a report: %v", err)
		}
		read = append(read, one)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the reports: %v", err)
	}
	return read
}

// ids is every report in the store by id, oldest first. A test names a report
// by reading it back here because nothing else can: the way in renders no id
// in the session that submitted, the channel carrying nothing back that could
// identify a submission later.
func (r *reports) ids(t *testing.T, ctx context.Context) []string {
	t.Helper()
	rows, err := r.reads.Query(ctx, `select id from `+reportstore.ReportTable+
		` order by collected_at, id`)
	if err != nil {
		t.Fatalf("reading the report ids: %v", err)
	}
	defer rows.Close()
	var read []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("reading a report id: %v", err)
		}
		read = append(read, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the report ids: %v", err)
	}
	return read
}

// counts is the counter keyed by service, the empty key being the whole
// channel, and the zero value where the store has counted nothing there.
func (r *reports) counts(t *testing.T, ctx context.Context, serviceID string) reportstore.Count {
	t.Helper()
	all, err := r.channel.store.Counts(ctx)
	if err != nil {
		t.Fatalf("reading the counters: %v", err)
	}
	for _, one := range all {
		if one.ServiceID == serviceID {
			return one
		}
	}
	return reportstore.Count{ServiceID: serviceID}
}

// overTheSocket is a client that reaches a deployed service's way in where it
// listens: the socket in the target's own directory, which is the whole of its
// address. It is the same client package wayin's own test dials with, written
// out again rather than shared across the two packages.
func overTheSocket(socket string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		},
	}
}

// waitForTheWayIn waits for the deployed service to be listening. The process
// was started by the deploy and its way in listens a moment later, so a test
// that dialled once would be reading a race rather than the channel.
func waitForTheWayIn(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", socket)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the way in of the deployed service never listened on %s", socket)
}
