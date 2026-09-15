package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/healthmonitor"
)

func writeFailureEmissions(t *testing.T, path, build, deploy, service, target string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("clearing failure emissions: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reading signal directory: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".signal") || entry.Name() == filepath.Base(path) {
			continue
		}
		if err := os.Remove(filepath.Join(filepath.Dir(path), entry.Name())); err != nil {
			t.Fatalf("clearing prior signal %s: %v", entry.Name(), err)
		}
	}
	for n := range 100 {
		at := time.Now().UTC()
		historyBuild := fmt.Sprintf("history-%02d", n)
		historyDeploy := deploy + "-history"
		historyAt := at.Add(-2 * time.Second)
		writeEmissionRecords(t, filepath.Join(filepath.Dir(path), historyBuild+".signal"), []healthmonitor.EmissionRecord{
			{Version: "emission/3", Kind: healthmonitor.RecordArrival, Time: historyAt, Service: service,
				Build: historyBuild, Deploy: historyDeploy, Target: target, Operation: "checkout", Deadline: time.Second},
			{Version: "emission/3", Kind: healthmonitor.RecordCompletion, Time: historyAt.Add(time.Millisecond), Service: service,
				Build: historyBuild, Deploy: historyDeploy, Target: target, Operation: "checkout", Outcome: "success", Duration: time.Millisecond},
		})
		records := make([]healthmonitor.EmissionRecord, 0, 60)
		for range 20 {
			records = append(records,
				healthmonitor.EmissionRecord{Version: "emission/3", Kind: healthmonitor.RecordArrival,
					Time: at, Service: service, Build: build, Deploy: deploy, Target: target, Operation: "checkout", Deadline: time.Second},
				healthmonitor.EmissionRecord{Version: "emission/3", Kind: healthmonitor.RecordCompletion,
					Time: at.Add(time.Millisecond), Service: service, Build: build, Deploy: deploy, Target: target,
					Operation: "checkout", Outcome: "failure", Duration: time.Millisecond,
					FailureClass: "synthetic", CodeLocation: "checkout"})
		}
		appendEmissionRecords(t, path, records)
		time.Sleep(55 * time.Millisecond)
	}
}

func appendEmissionRecords(t *testing.T, path string, records []healthmonitor.EmissionRecord) {
	t.Helper()
	data := make([]byte, 0, len(records)*180)
	for _, one := range records {
		line, err := storedEmission(one)
		if err != nil {
			t.Fatalf("encoding failure emission: %v", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening failure emissions: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatalf("writing failure emissions: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing failure emissions: %v", err)
	}
}

func writeEmissionRecords(t *testing.T, path string, records []healthmonitor.EmissionRecord) {
	t.Helper()
	data := make([]byte, 0, len(records)*180)
	for _, one := range records {
		line, err := storedEmission(one)
		if err != nil {
			t.Fatalf("encoding emission: %v", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing emission: %v", err)
	}
}

func storedEmission(one healthmonitor.EmissionRecord) ([]byte, error) {
	return json.Marshal(struct {
		AcceptedAt time.Time                    `json:"accepted_at"`
		Record     healthmonitor.EmissionRecord `json:"record"`
	}{AcceptedAt: one.Time, Record: one})
}
