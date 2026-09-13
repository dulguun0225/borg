package policy_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

func TestConcurrentAuthoringCarriesBothValuesForward(t *testing.T) {
	ctx, in := newFactory(t)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	blocker, err := in.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, `select pg_advisory_xact_lock($1)`, policy.AdvisoryLockKey()); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3); results <- err }()
	go func() { _, err := in.factory.AuthorWindowCap(ctx, owner, in.service.ID, 600); results <- err }()
	// Both writers must have reached the same lock before either proceeds.
	for {
		var waiting int
		err := in.pool.QueryRow(ctx, `select count(*) from pg_locks where locktype = 'advisory'
			and not granted and classid::bigint = $1 and objid::bigint = $2`,
			policy.AdvisoryLockKey()>>32, policy.AdvisoryLockKey()&0xffffffff).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	newest := newestVersion(t, ctx, in)
	found := map[gatepolicy.Parameter]float64{}
	for _, value := range newest.Authored {
		if value.Scope.ID == in.service.ID {
			found[value.Parameter] = value.Number
		}
	}
	if found[gatepolicy.WindowLimit] != 3 || found[gatepolicy.WindowCap] != 600 {
		t.Fatalf("concurrent writes lost authored state: %+v", newest.Authored)
	}
}

func TestCreationRetryAfterOtherWriteKeepsItsRecord(t *testing.T) {
	ctx, in := newFactory(t)
	first, version, err := in.factory.DeclareArea(ctx, owner, "repeatable", area.InsideProject(in.project.ID), area.Hazard{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3); err != nil {
		t.Fatal(err)
	}
	second, retried, err := in.factory.DeclareArea(ctx, owner, "repeatable", area.InsideProject(in.project.ID), area.Hazard{})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || retried.ID != version.ID {
		t.Fatalf("retry created another record: %s / %s", first.ID, second.ID)
	}
}

// Block after the first settings query has read its result. A policy write can
// then commit before the gate reads the threshold, forcing a mixed snapshot.
type pauseSettingsRead struct {
	once         sync.Once
	read, resume chan struct{}
}

func (p *pauseSettingsRead) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, p, strings.Contains(data.SQL, "from "+factorysettings.Table+" where only_row"))
}
func (p *pauseSettingsRead) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if matched, _ := ctx.Value(p).(bool); matched {
		p.once.Do(func() {
			close(p.read)
			select {
			case <-p.resume:
			case <-ctx.Done():
			}
		})
	}
}

func TestAtGateRetriesWhenPolicyChangesDuringItsReads(t *testing.T) {
	ctx, in := newFactory(t)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", .2); err != nil {
		t.Fatal(err)
	}
	pause := &pauseSettingsRead{read: make(chan struct{}), resume: make(chan struct{})}
	config := in.pool.Config()
	config.ConnConfig.Tracer = pause
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reader := policy.NewReader(pool, in.token, score.Version{})
	result := make(chan policy.Applied, 1)
	failed := make(chan error, 1)
	go func() {
		applied, err := reader.AtGate(ctx, ownerReading, in.subjects("merge_to_master"))
		result <- applied
		failed <- err
	}()
	select {
	case <-pause.read:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	written, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", .6)
	close(pause.resume)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-failed; err != nil {
		t.Fatal(err)
	}
	applied := <-result
	if applied.PolicyVersion != written.ID || applied.Threshold != .6 {
		t.Fatalf("gate mixed the policy version and threshold: %+v; want %s / .6", applied, written.ID)
	}
}
