package localtarget_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/targetseam"
)

const signalSource = `package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("{\"version\":\"emission/3\",\"kind\":\"arrival\",\"time\":\"2026-01-01T00:00:00Z\"}")
	time.Sleep(time.Hour)
}
`

func TestDeployAcceptsSignalLinesWithTheTargetTime(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", signalSource)

	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential,
		Configuration: deployIDConfig("dep_1"),
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	path := localtarget.SignalFile(dir, "rel_one")
	waitForFile(t, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading accepted signal: %v", err)
	}
	var stored struct {
		AcceptedAt time.Time       `json:"accepted_at"`
		Record     json.RawMessage `json:"record"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("decoding accepted signal: %v", err)
	}
	if stored.AcceptedAt.IsZero() {
		t.Fatal("accepted signal has no target acceptance time")
	}
	var record map[string]any
	if err := json.Unmarshal(stored.Record, &record); err != nil {
		t.Fatalf("decoding accepted record: %v", err)
	}
	if record["version"] != "emission/3" || record["kind"] != "arrival" {
		t.Fatalf("accepted record = %+v, want the process record", record)
	}
}
