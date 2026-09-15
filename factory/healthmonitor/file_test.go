package healthmonitor_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
)

func TestM11FileEmissionReadsRecordsByOperationAndDeploy(t *testing.T) {
	dir := t.TempDir()
	path := func(target, build string) string { return filepath.Join(target, build+".signal") }
	reader := healthmonitor.NewFileEmission(dir, path)
	base := time.Now().Add(-3 * time.Second).Truncate(50 * time.Millisecond)
	release := []healthmonitor.EmissionRecord{
		{Version: "emission/3", Kind: healthmonitor.RecordArrival, Time: base, Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "checkout", Deadline: time.Second},
		{Version: "emission/3", Kind: healthmonitor.RecordArrival, Time: base.Add(50 * time.Millisecond), Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "checkout", Deadline: time.Second},
		{Version: "emission/3", Kind: healthmonitor.RecordArrival, Time: base.Add(100 * time.Millisecond), Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "checkout", Deadline: time.Second},
		{Version: "emission/3", Kind: healthmonitor.RecordArrival, Time: base.Add(100 * time.Millisecond), Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "search", Deadline: time.Second},
		{Version: "emission/3", Kind: healthmonitor.RecordCompletion, Time: base.Add(2 * time.Second), Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "checkout", Outcome: "ok", Duration: 2 * time.Second},
		{Version: "emission/3", Kind: healthmonitor.RecordCompletion, Time: base.Add(50*time.Millisecond + 2*time.Second), Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "checkout", Outcome: "error", Duration: 2 * time.Second},
		{Version: "emission/3", Kind: healthmonitor.RecordCompletion, Time: base.Add(50 * time.Millisecond), Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "checkout", Outcome: "failed", Duration: time.Millisecond, FailureClass: "timeout", CodeLocation: "handler.go:12"},
		{Version: "emission/3", Kind: healthmonitor.RecordHazardousOperation, Time: base.Add(100 * time.Millisecond), Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "charge", HazardousCount: 1},
	}
	control := []healthmonitor.EmissionRecord{
		{Version: "emission/3", Kind: healthmonitor.RecordArrival, Time: base, Service: "orders", Build: "good", Deploy: "deploy-good", Target: dir, Operation: "checkout", Deadline: time.Second},
		{Version: "emission/3", Kind: healthmonitor.RecordCompletion, Time: base.Add(2 * time.Second), Service: "orders", Build: "good", Deploy: "deploy-good", Target: dir, Operation: "checkout", Outcome: "ok", Duration: 2 * time.Second},
	}
	writeEmission(t, path(dir, "bad"), release)
	writeEmission(t, path(dir, "good"), control)
	series, err := reader.Read(context.Background(), healthmonitor.Reading{
		ServiceName: "orders", Target: dir,
		Release:             healthmonitor.Arm{BuildID: "bad", DeployID: "deploy-bad"},
		Baseline:            healthmonitor.Arm{BuildID: "good", DeployID: "deploy-good"},
		OperationsReadAlone: []string{"checkout"},
	})
	if err != nil {
		t.Fatalf("reading emission: %v", err)
	}
	checkout := operation(t, series, "checkout")
	errors := checkout.Quantities[gatepolicy.QuantityErrorRate].Intervals
	errorsTotal := checkout.Quantities[gatepolicy.QuantityErrorRate].Totals()
	if errorsTotal.Units != 3 || errorsTotal.Count != 2 || errorsTotal.BaselineUnits != 1 || errorsTotal.BaselineCount != 0 {
		t.Fatalf("checkout error intervals = %+v, want failed completions and unfinished arrivals over arrivals", errors)
	}
	requests := checkout.Quantities[gatepolicy.QuantityRequestRate].Intervals
	requestsTotal := checkout.Quantities[gatepolicy.QuantityRequestRate].Totals()
	if requestsTotal.Units != 4 || requestsTotal.Count != 3 || requestsTotal.BaselineUnits != 4 || requestsTotal.BaselineCount != 1 {
		t.Fatalf("checkout request intervals = %+v, want each arm's arrivals over shared traffic", requests)
	}
	outcomes := map[string]bool{}
	for _, one := range checkout.Histogram {
		outcomes[one.Outcome] = true
	}
	if len(outcomes) != 3 || checkout.LatencyBucketShare <= 0 {
		t.Fatalf("checkout histogram = %+v, bucket width %v; want one histogram per outcome and quantile bucket", checkout.Histogram, checkout.LatencyBucketShare)
	}
	pooled := operation(t, series, healthmonitor.PooledOperation)
	if pooled.Quantities[gatepolicy.QuantityRequestRate].Totals().Units != 1 {
		t.Errorf("pooled request intervals = %+v, want the search arrival", pooled.Quantities[gatepolicy.QuantityRequestRate].Intervals)
	}
	hazardous := operation(t, series, healthmonitor.PooledOperation)
	hazardousTotal := hazardous.Quantities[gatepolicy.QuantityHazardousOperation].Totals()
	if hazardousTotal.Units != 2 || hazardousTotal.Count != 1 {
		t.Errorf("hazardous operation intervals = %+v, want the operation count over arrivals plus operations", hazardous.Quantities[gatepolicy.QuantityHazardousOperation].Intervals)
	}
}

func TestM11FileEmissionKeepsFailureRecordKey(t *testing.T) {
	dir := t.TempDir()
	path := func(target, build string) string { return filepath.Join(target, build+".signal") }
	reader := healthmonitor.NewFileEmission(dir, path)
	when := time.Now().Add(-2 * time.Second).Truncate(50 * time.Millisecond)
	writeEmission(t, path(dir, "bad"), []healthmonitor.EmissionRecord{
		{Version: "emission/3", Kind: healthmonitor.RecordCompletion, Time: when, Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "checkout", Outcome: "failed", Duration: time.Millisecond, FailureClass: "timeout", CodeLocation: "handler.go:12"},
		{Version: "emission/3", Kind: healthmonitor.RecordCompletion, Time: when, Service: "orders", Build: "bad", Deploy: "deploy-bad", Target: dir, Operation: "checkout", Outcome: "failed", Duration: time.Millisecond, FailureClass: "timeout", CodeLocation: "handler.go:12"},
	})
	records, err := reader.FailureRecords(context.Background(), healthmonitor.Reading{
		ServiceName: "orders", Target: dir, Release: healthmonitor.Arm{BuildID: "bad", DeployID: "deploy-bad"},
	})
	if err != nil {
		t.Fatalf("reading failure records: %v", err)
	}
	if len(records) != 1 || records[0].Count != 2 || records[0].FailureClass != "timeout" || records[0].CodeLocation != "handler.go:12" {
		t.Fatalf("failure records = %+v, want one complete key with count two", records)
	}
}

func TestM11FileEmissionReportsTheShippedShape(t *testing.T) {
	dir := t.TempDir()
	path := func(target, build string) string { return filepath.Join(target, build+".signal") }
	writeEmission(t, path(dir, "build"), []healthmonitor.EmissionRecord{{
		Version: "emission/3", Kind: healthmonitor.RecordArrival, Time: time.Now(), Build: "build",
	}})
	reader := healthmonitor.NewFileEmission(dir, path)
	version, err := reader.Shape(context.Background(), healthmonitor.Arm{BuildID: "build"})
	if err != nil || version != "emission/3" {
		t.Fatalf("Shape = %q, %v; want emission/3", version, err)
	}
	shape, found := healthmonitor.ShapeAt(version)
	if !found || len(shape.HistogramBoundaries) == 0 || shape.Quantile <= 0 {
		t.Fatalf("shape = %+v, found %t; want fixed histogram and quantile", shape, found)
	}
}

func operation(t *testing.T, series healthmonitor.Series, name string) healthmonitor.OperationSeries {
	t.Helper()
	for _, one := range series.Operations {
		if one.Operation == name {
			return one
		}
	}
	t.Fatalf("operation %q absent from %+v", name, series.Operations)
	return healthmonitor.OperationSeries{}
}

func writeEmission(t *testing.T, path string, records []healthmonitor.EmissionRecord) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating emission: %v", err)
	}
	for _, one := range records {
		encoded, err := json.Marshal(struct {
			AcceptedAt time.Time                    `json:"accepted_at"`
			Record     healthmonitor.EmissionRecord `json:"record"`
		}{AcceptedAt: one.Time, Record: one})
		if err != nil {
			t.Fatalf("encoding emission: %v", err)
		}
		if _, err := file.Write(append(encoded, '\n')); err != nil {
			t.Fatalf("writing emission: %v", err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing emission: %v", err)
	}
}
