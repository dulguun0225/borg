package localtarget

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type signalFailures struct {
	mu    sync.Mutex
	first error
	later int
}

func (s *signalFailures) record(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.first == nil {
		s.first = err
		return
	}
	s.later++
}

func (s *signalFailures) take() (error, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err, later := s.first, s.later
	s.first, s.later = nil, 0
	return err, later
}

// CountSignal reads the adoption path's reachability evidence from the target
// file. It counts arrivals and failed completions without becoming a health
// monitor quantity reader.
func CountSignal(path string) (int64, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	var units, failures int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var stored acceptedSignal
		if json.Unmarshal([]byte(line), &stored) == nil && len(stored.Record) > 0 {
			var record struct {
				Kind         string `json:"kind"`
				FailureClass string `json:"failure_class"`
			}
			if json.Unmarshal(stored.Record, &record) == nil {
				if record.Kind == "arrival" {
					units++
				}
				if record.Kind == "completion" && record.FailureClass != "" {
					failures++
				}
				continue
			}
			var legacy string
			if json.Unmarshal(stored.Record, &legacy) == nil {
				units++
				if strings.HasSuffix(legacy, "\terror") || strings.HasSuffix(legacy, "\tfailure") || legacy == "error" || legacy == "failure" {
					failures++
				}
				continue
			}
		}
		units++
		if strings.HasSuffix(line, "\terror") || strings.HasSuffix(line, "\tfailure") || line == "error" || line == "failure" {
			failures++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	return units, failures, nil
}

// acceptedSignal is the local store's envelope. The process supplies record;
// the target supplies AcceptedAt when it accepts the line from the process.
type acceptedSignal struct {
	AcceptedAt time.Time       `json:"accepted_at"`
	Deploy     string          `json:"deploy,omitempty"`
	Record     json.RawMessage `json:"record"`
}

func (l *Local) acceptSignals(output io.Reader, path, deploy string) error {
	var file *os.File
	var writer *bufio.Writer
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		data := json.RawMessage(append([]byte(nil), line...))
		if !json.Valid(data) {
			encoded, err := json.Marshal(string(line))
			if err != nil {
				return err
			}
			data = encoded
		}
		if file == nil {
			var err error
			file, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			writer = bufio.NewWriter(file)
		}
		stored, err := json.Marshal(acceptedSignal{AcceptedAt: time.Now().UTC(), Deploy: deploy, Record: data})
		if err != nil {
			return err
		}
		_, writeErr := writer.Write(append(stored, '\n'))
		if writeErr == nil {
			writeErr = writer.Flush()
		}
		if writeErr != nil {
			return writeErr
		}
	}
	scanErr := scanner.Err()
	if scanErr != nil && !errors.Is(scanErr, os.ErrClosed) && !strings.Contains(scanErr.Error(), "file already closed") {
		return scanErr
	}
	return nil
}

func (l *Local) startSignalReader(output io.Reader, path, deploy string) {
	go func() {
		if err := l.acceptSignals(output, path, deploy); err != nil {
			l.signal.record(err)
		}
	}()
}

func (l *Local) signalError() error {
	err, later := l.signal.take()
	if err == nil {
		return nil
	}
	if later > 0 {
		return fmt.Errorf("localtarget: accepting signal: %w (and %d later acceptance error(s))", err, later)
	}
	return fmt.Errorf("localtarget: accepting signal: %w", err)
}
