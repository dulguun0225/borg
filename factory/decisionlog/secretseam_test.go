package decisionlog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// TestAResolvedSecretReachesNoRecord is seam 3 meeting seam 2: the value is
// resolved and used, the record names the reference, and the bytes in the
// table are searched for the value rather than the Go values being trusted.
func TestAResolvedSecretReachesNoRecord(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	const value = "sk-the-value-nothing-else-may-see"
	path := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(path, []byte("deploy.staging="+value+"\n"), 0o600); err != nil {
		t.Fatalf("writing the secrets file: %v", err)
	}
	resolver, err := secretref.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	credential := secretref.MustNew("deploy.staging")
	deployer := principal.OfComponent("deployer")
	resolved, err := resolver.Resolve(deployer, credential)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != value {
		t.Fatalf("Resolve = %q, want the value", resolved)
	}
	resolutions := resolver.Resolutions()
	if len(resolutions) != 1 || resolutions[0].Principal != deployer {
		t.Fatalf("Resolutions() = %+v, want one naming %s", resolutions, deployer)
	}

	// What a component writes about a deploy: the reference, which is what
	// it has, because the value it resolved is used at the moment it
	// connects and goes into nothing that is stored.
	if _, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor:         gate,
		Payload:       `{"gate":"deploy","credential":"` + credential.Name() + `"}`,
		FormatVersion: "decision/1",
		PolicyVersion: "policy-1",
		ScoreVersion:  "score-1",
	}); err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: owner, Payload: "the deploy used " + credential.String(), FormatVersion: "page_event/1",
	}); err != nil {
		t.Fatalf("AppendPageEvent: %v", err)
	}

	// Every column of every row, as the database holds it. Casting the row
	// to text is what makes this a claim about the stored bytes and not
	// about the columns the test remembered to name.
	var holding int
	if err := pool.QueryRow(ctx,
		`select count(*) from decision_log r where strpos(r::text, $1) > 0`, value,
	).Scan(&holding); err != nil {
		t.Fatalf("searching the rows: %v", err)
	}
	if holding != 0 {
		t.Fatalf("%d rows hold the secret value", holding)
	}

	var naming int
	if err := pool.QueryRow(ctx,
		`select count(*) from decision_log r where strpos(r::text, $1) > 0`, credential.Name(),
	).Scan(&naming); err != nil {
		t.Fatalf("searching the rows: %v", err)
	}
	if naming != 2 {
		t.Fatalf("%d rows name the reference, want 2 — the search found nothing to trust", naming)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}
