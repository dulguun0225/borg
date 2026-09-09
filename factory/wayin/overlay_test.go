package wayin_test

import (
	"context"
	"encoding/json"
	"go/format"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/wayin"
)

func TestSourceFillsTheIdentityAndIsGoTheToolchainFormats(t *testing.T) {
	source, err := wayin.Source("factory/3")
	if err != nil {
		t.Fatalf("filling the shipped source: %v", err)
	}
	if !strings.Contains(source, `= "factory/3"`) {
		t.Errorf("the filled source does not name the identity it was given")
	}
	if strings.Contains(source, "shipped-bundle-identity\"") {
		t.Errorf("the filled source still carries the substitution point")
	}
	formatted, err := format.Source([]byte(source))
	if err != nil {
		t.Fatalf("the shipped source does not parse: %v", err)
	}
	if string(formatted) != source {
		t.Errorf("the shipped source is not formatted as the toolchain formats it")
	}
}

func TestSourceRefusesAnEmptyIdentity(t *testing.T) {
	if _, err := wayin.Source(""); err == nil {
		t.Errorf("a source naming no shipped-bundle identity was filled, want an error")
	}
}

func TestOverlayWritesOutsideTheCheckout(t *testing.T) {
	checkout, dir := t.TempDir(), t.TempDir()

	path, err := wayin.Overlay(checkout, dir, "factory/3")
	if err != nil {
		t.Fatalf("writing the overlay: %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, wayin.FileName)); !os.IsNotExist(err) {
		t.Errorf("the way in was written into the checkout, and nothing may be")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the overlay: %v", err)
	}
	var read struct {
		Replace map[string]string `json:"Replace"`
	}
	if err := json.Unmarshal(content, &read); err != nil {
		t.Fatalf("reading the overlay %q: %v", content, err)
	}
	inCheckout := filepath.Join(checkout, wayin.FileName)
	backing, ok := read.Replace[inCheckout]
	if !ok {
		t.Fatalf("the overlay maps %v, want %s", read.Replace, inCheckout)
	}
	if backing != filepath.Join(dir, wayin.FileName) {
		t.Errorf("the overlay backs the file with %s, want the one written outside the checkout", backing)
	}
}

// TestTheOverlayBuildsAndTheWayInServesTheNotice is the one test that proves
// the shipped source and the overlay rather than describing them: it writes a
// module the factory did not author, builds it with the overlay, and runs the
// binary against a real entrance. It takes a few seconds and is not skipped —
// nothing else here would fail if the file the factory injects stopped
// compiling inside somebody else's main.
func TestTheOverlayBuildsAndTheWayInServesTheNotice(t *testing.T) {
	store := &fakeStore{
		notice: wayin.Notice{ID: "con_1", Text: "what this channel is for"},
		result: wayin.Result{Accepted: true},
	}
	entrance := httptest.NewServer(wayin.NewEntrance(store))
	defer entrance.Close()

	client := wayInAt(t, buildWithTheWayIn(t), entrance.URL)

	shown := openSession(t, client)
	if shown.Text != "what this channel is for" || shown.NoticeID != "con_1" {
		t.Errorf("the way in showed %+v, want the notice the store holds", shown)
	}
	if shown.Session == "" {
		t.Errorf("the way in showed no session key, and a submission carries one back")
	}
	if len(store.noticeFor) == 0 || store.noticeFor[0] != "token-1" {
		t.Errorf("the entrance was asked for %v, want the token the way in was deployed with", store.noticeFor)
	}

	var result struct {
		Accepted bool   `json:"accepted"`
		Refusal  string `json:"refusal"`
	}
	post(t, client, `{"kind":"bug","text":"the save button does nothing","session":"`+shown.Session+
		`","notice_id":"`+shown.NoticeID+`"}`, &result)
	if !result.Accepted {
		t.Errorf("the way in rendered %+v, want what the store answered", result)
	}
	if len(store.submitted) != 1 {
		t.Fatalf("the entrance took %d submissions, want one", len(store.submitted))
	}
	sub := store.submitted[0]
	if sub.Token != "token-1" || sub.ShippedBundleIdentity != "factory/3" || sub.Shape != wayin.Shape {
		t.Errorf("the store was handed %+v, want the token, the identity and the shape the way in ships", sub)
	}
	if sub.SourceKey == "" || sub.SourceKey == shown.Session {
		t.Errorf("the source key is %q, want one derived from the session and not the session",
			sub.SourceKey)
	}
}

// TestTheSourceKeyMarksOneSessionOfOneProcess is the whole of what the key is
// for, and it is asserted against the built binary because the derivation is
// the deployed software's: a session's reports are one source's, a fresh
// session is a fresh key, and nothing joins the sessions of two processes.
// The same session value is submitted to both, so the only thing that differs
// between the two keys is the salt each process minted.
func TestTheSourceKeyMarksOneSessionOfOneProcess(t *testing.T) {
	store := &fakeStore{result: wayin.Result{Accepted: true}}
	entrance := httptest.NewServer(wayin.NewEntrance(store))
	defer entrance.Close()

	binary := buildWithTheWayIn(t)
	one := wayInAt(t, binary, entrance.URL)
	other := wayInAt(t, binary, entrance.URL)
	session, second := openSession(t, one).Session, openSession(t, one).Session
	if session == "" || session == second {
		t.Fatalf("two opens minted %q and %q, want a fresh session at each", session, second)
	}

	submitUnder(t, one, session)
	submitUnder(t, one, session)
	submitUnder(t, one, second)
	submitUnder(t, other, session)

	if len(store.submitted) != 4 {
		t.Fatalf("the entrance took %d submissions, want four", len(store.submitted))
	}
	keys := []string{store.submitted[0].SourceKey, store.submitted[1].SourceKey,
		store.submitted[2].SourceKey, store.submitted[3].SourceKey}
	if keys[0] == "" {
		t.Fatalf("the way in supplied no key at all")
	}
	if keys[0] != keys[1] {
		t.Errorf("one session was keyed %q and then %q, want one key for the session", keys[0], keys[1])
	}
	if keys[0] == keys[2] {
		t.Errorf("two sessions of one process share the key %q, and a fresh session is a fresh key", keys[0])
	}
	if keys[0] == keys[3] {
		t.Errorf("two processes keyed one session the same, and a process that starts again re-keys")
	}
}

// TestTheWayInStartsNothingWhereTheFactoryNamedNothing is the other half: a
// service started outside the factory runs with the file in its build and
// serves nothing at all.
func TestTheWayInStartsNothingWhereTheFactoryNamedNothing(t *testing.T) {
	binary := buildWithTheWayIn(t)
	socket := filepath.Join(shortDir(t), "way-in.sock")
	started := start(t, binary)

	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Errorf("a service the factory named nothing to listens on %s", socket)
	}
	stderr := started(t)
	if lines := strings.Count(strings.TrimSpace(stderr), "\n") + 1; lines != 1 {
		t.Errorf("the way in wrote %d lines where the three names are unset, want one:\n%s", lines, stderr)
	}
	if !strings.Contains(stderr, "BORG_WAY_IN") {
		t.Errorf("the one line the way in wrote is %q, want the names it read", stderr)
	}
}

// buildWithTheWayIn writes a module of its own, with a main that is nobody's
// but its own, and builds it with the overlay. Its go directive is far older
// than the factory's, which is what a service the factory deploys may have
// and what decides both the routing the shipped source gets and the spellings
// it may use.
func buildWithTheWayIn(t *testing.T) string {
	t.Helper()
	checkout := t.TempDir()
	write(t, filepath.Join(checkout, "go.mod"), "module service\n\ngo 1.16\n")
	write(t, filepath.Join(checkout, "main.go"), `package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("the service started")
	time.Sleep(time.Minute)
}
`)
	overlay, err := wayin.Overlay(checkout, t.TempDir(), "factory/3")
	if err != nil {
		t.Fatalf("writing the overlay: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "service")
	build := exec.Command("go", "build", "-overlay", overlay, "-o", binary, ".")
	build.Dir = checkout
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the service with the way in: %v: %s", err, out)
	}
	return binary
}

// start runs the built service with env beside the environment, and returns
// what ends it and answers with what it wrote to standard error.
func start(t *testing.T, binary string, env ...string) func(*testing.T) string {
	t.Helper()
	var out, errors strings.Builder
	service := exec.Command(binary)
	service.Env = append(os.Environ(), env...)
	service.Stdout, service.Stderr = &out, &errors
	if err := service.Start(); err != nil {
		t.Fatalf("starting the service: %v", err)
	}
	ended := false
	end := func(t *testing.T) string {
		t.Helper()
		if !ended {
			ended = true
			_ = service.Process.Kill()
			_ = service.Wait()
		}
		if !strings.Contains(out.String(), "the service started") {
			t.Errorf("the service did not start: %q %q", out.String(), errors.String())
		}
		return errors.String()
	}
	t.Cleanup(func() { end(t) })
	return end
}

// shortDir is a directory outside the test's own, because a Unix socket's
// path is bounded at about a hundred characters and a test's temporary
// directory is named after the test.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wayin-")
	if err != nil {
		t.Fatalf("making a directory for the socket: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// wayInAt starts one process of the built service against entrance and
// returns a client that reaches its way in.
func wayInAt(t *testing.T, binary, entrance string) *http.Client {
	t.Helper()
	socket := filepath.Join(shortDir(t), "way-in.sock")
	// The three names are the exported ones and not literals: what this test
	// starts is the shipped source, so setting them through the constants a
	// deploy target sets is what holds the two spellings together.
	start(t, binary, wayin.TokenEnv+"=token-1", wayin.StoreEnv+"="+entrance,
		wayin.ListenEnv+"="+socket)
	waitForTheSocket(t, socket)
	return overTheSocket(socket)
}

// shownAtTheOpen is what a session is given at the open.
type shownAtTheOpen struct {
	NoticeID string `json:"notice_id"`
	Text     string `json:"text"`
	Session  string `json:"session"`
}

// openSession reads the notice, which is what mints a session.
func openSession(t *testing.T, client *http.Client) shownAtTheOpen {
	t.Helper()
	var shown shownAtTheOpen
	get(t, client, &shown)
	return shown
}

// submitUnder submits one report in the session that was shown at the open.
func submitUnder(t *testing.T, client *http.Client, session string) {
	t.Helper()
	var result struct {
		Accepted bool `json:"accepted"`
	}
	post(t, client, `{"kind":"bug","text":"the save button does nothing","session":"`+session+
		`","notice_id":""}`, &result)
	if !result.Accepted {
		t.Fatalf("a submission in session %q was not accepted", session)
	}
}

func waitForTheSocket(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", socket)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the way in never listened on %s", socket)
}

// overTheSocket is a client that reaches the way in where it listens: the
// socket in the target's own directory, which is the whole of its address.
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

func get(t *testing.T, client *http.Client, into any) {
	t.Helper()
	response, err := client.Get("http://way-in/")
	if err != nil {
		t.Fatalf("reading the notice: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the notice answered %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		t.Fatalf("reading the notice: %v", err)
	}
}

func post(t *testing.T, client *http.Client, body string, into any) {
	t.Helper()
	response, err := client.Post("http://way-in/", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the submission answered %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		t.Fatalf("reading the submit result: %v", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
