// The extractor over a real repository: one commit as the base, one as the
// head, and the four kinds read out of the diff between them.
package exposure_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/exposure"
)

// repoWith is a git repository whose first commit is base and whose second is
// head, each a map from file name to content. It returns the directory, the base
// commit and the head commit, which is what [exposure.Derive] is asked about.
func repoWith(t *testing.T, base, head map[string]string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-b", "master")
	run(t, dir, "config", "user.name", "test")
	run(t, dir, "config", "user.email", "test@factory.invalid")

	write(t, dir, base)
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-m", "base")
	first := run(t, dir, "rev-parse", "HEAD")

	write(t, dir, head)
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-m", "head")
	second := run(t, dir, "rev-parse", "HEAD")
	return dir, first, second
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making the directory for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// derive is the evidence for the two commits, failing the test on an error —
// which is a caller's defect here, git's own refusals landing on Unavailable.
func derive(t *testing.T, c exposure.Checkout, base, head string) exposure.Evidence {
	t.Helper()
	evidence, coverage, err := exposure.Derive(context.Background(), c, base, head)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if coverage.Toolchain != exposure.Toolchain {
		t.Errorf("the coverage names toolchain %q, want %q", coverage.Toolchain, exposure.Toolchain)
	}
	if evidence.Unavailable == "" && coverage.Changes == 0 {
		t.Errorf("the coverage read no changed line and the derivation reports no reason")
	}
	return evidence
}

// TestADiffThatReachesNothingNewReadsAsNothing: a diff adding none of the four
// kinds reads as nothing, and never as unavailable. The two are opposite
// answers — nothing lowers the number and unavailable resolves the factor — so
// this is the case the whole distinction rests on.
func TestADiffThatReachesNothingNewReadsAsNothing(t *testing.T) {
	dir, base, head := repoWith(t,
		map[string]string{
			"go.mod":  "module demo\n\ngo 1.25\n",
			"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"ok\") }\n",
		},
		map[string]string{
			"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"still ok\") }\n",
		})

	evidence := derive(t, exposure.Checkout{Dir: dir}, base, head)
	if evidence.Unavailable != "" {
		t.Fatalf("the derivation is unavailable: %s", evidence.Unavailable)
	}
	if list := evidence.List(); len(list) != 0 {
		t.Errorf("a diff reaching nothing new reads as %v", list)
	}
}

// TestAnOutboundCallAdded: a new import of one of the packages a Go build
// reaches the world through, and a new call to an http client's method.
func TestAnOutboundCallAdded(t *testing.T) {
	dir, base, head := repoWith(t,
		map[string]string{"main.go": "package main\n\nfunc main() {}\n"},
		map[string]string{"main.go": "package main\n\nimport \"net/http\"\n\n" +
			"func main() {\n\tclient := &http.Client{}\n\t_, _ = client.Do(nil)\n}\n"})

	evidence := derive(t, exposure.Checkout{Dir: dir}, base, head)
	if len(evidence.OutboundCalls) != 2 {
		t.Fatalf("the outbound calls found are %v, want the import and the client call", evidence.OutboundCalls)
	}
	if !strings.Contains(evidence.OutboundCalls[0], "main.go:3") ||
		!strings.Contains(evidence.OutboundCalls[0], "net/http") {
		t.Errorf("the first entry is %q, want the file, the line and the package", evidence.OutboundCalls[0])
	}
	if !strings.Contains(evidence.OutboundCalls[1], "client.Do") {
		t.Errorf("the second entry is %q, want the http client method", evidence.OutboundCalls[1])
	}
}

// TestACredentialNamedOrRead: a read of an environment variable whose name
// carries one of the credential words, and a string literal shaped like a
// secret's name.
func TestACredentialNamedOrRead(t *testing.T) {
	dir, base, head := repoWith(t,
		map[string]string{"main.go": "package main\n\nfunc main() {}\n"},
		map[string]string{"main.go": "package main\n\nimport \"os\"\n\n" +
			"func main() {\n\t_ = os.Getenv(\"PAYMENT_API_TOKEN\")\n\t_ = \"model.openrouter\"\n}\n"})

	evidence := derive(t, exposure.Checkout{Dir: dir}, base, head)
	if len(evidence.Credentials) != 2 {
		t.Fatalf("the credentials found are %v, want the read and the name", evidence.Credentials)
	}
	if !strings.Contains(evidence.Credentials[0], "PAYMENT_API_TOKEN") ||
		!strings.Contains(evidence.Credentials[0], "TOKEN") {
		t.Errorf("the first entry is %q, want the variable and the word that made it one", evidence.Credentials[0])
	}
	if !strings.Contains(evidence.Credentials[1], "model.openrouter") {
		t.Errorf("the second entry is %q, want the literal shaped like a secret's name", evidence.Credentials[1])
	}
	// A variable read whose name carries none of the words is not a credential
	// read: the factor is what the change reaches and not every environment.
	if strings.Contains(strings.Join(evidence.List(), " "), "BORG_SIGNAL") {
		t.Errorf("an ordinary environment read is in the list: %v", evidence.List())
	}
}

// TestAnAuthorizationCheckRemovedOrWeakened: a removed call to a function whose
// name carries one of the authorization words, and the guard around one removed
// with it.
func TestAnAuthorizationCheckRemovedOrWeakened(t *testing.T) {
	dir, base, head := repoWith(t,
		map[string]string{"auth.go": "package main\n\nfunc serve() {\n" +
			"\tif !Authorize(\"read\") {\n\t\treturn\n\t}\n\twork()\n}\n"},
		map[string]string{"auth.go": "package main\n\nfunc serve() {\n\twork()\n}\n"})

	evidence := derive(t, exposure.Checkout{Dir: dir}, base, head)
	if len(evidence.AuthorizationChecks) != 1 {
		t.Fatalf("the authorization checks found are %v, want the removed guard", evidence.AuthorizationChecks)
	}
	if !strings.Contains(evidence.AuthorizationChecks[0], "Authorize") ||
		!strings.Contains(evidence.AuthorizationChecks[0], "auth.go:4") {
		t.Errorf("the entry is %q, want the file, the line and the check", evidence.AuthorizationChecks[0])
	}
}

// TestADependencyChange: a package added to go.mod and go.sum is one dependency
// change and not three, and it carries the licence the build's resolved set
// gives it.
func TestADependencyChange(t *testing.T) {
	dir, base, head := repoWith(t,
		map[string]string{"go.mod": "module demo\n\ngo 1.25\n"},
		map[string]string{
			"go.mod": "module demo\n\ngo 1.25\n\nrequire example.com/x v1.2.3\n",
			"go.sum": "example.com/x v1.2.3 h1:aaa=\nexample.com/x v1.2.3/go.mod h1:bbb=\n",
		})

	evidence := derive(t, exposure.Checkout{
		Dir:      dir,
		Resolved: []exposure.Package{{Package: "example.com/x", Version: "v1.2.3", Licence: "MIT"}},
	}, base, head)
	if len(evidence.DependencyChanges) != 1 {
		t.Fatalf("the dependency changes found are %v, want the one package", evidence.DependencyChanges)
	}
	if !strings.Contains(evidence.DependencyChanges[0], "example.com/x v1.2.3") ||
		!strings.Contains(evidence.DependencyChanges[0], "licence MIT") {
		t.Errorf("the entry is %q, want the package, the version and the licence", evidence.DependencyChanges[0])
	}
	// The module line and the go directive name no package and no version, so
	// neither is a dependency change.
	if strings.Contains(evidence.DependencyChanges[0], "module") {
		t.Errorf("the module line reads as a dependency change: %q", evidence.DependencyChanges[0])
	}
}

// TestADependencyChangeIsTwoResolvedSetsDiffed: what a build resolved against
// what the current release's build resolved. A package at the version already
// running is no change however its manifest line moved, a package at another
// version is one, and an unpinned range that resolved differently with nothing
// in the manifest changed is read as the change it is.
func TestADependencyChangeIsTwoResolvedSetsDiffed(t *testing.T) {
	dir, base, head := repoWith(t,
		map[string]string{
			"go.mod": "module demo\n\ngo 1.25\n\nrequire example.com/x v1.2.3\n",
			"go.sum": "example.com/x v1.2.3 h1:aaa=\n",
		},
		map[string]string{
			"go.mod": "module demo\n\ngo 1.25\n\nrequire example.com/x v1.2.3 // moved comment\n",
			"go.sum": "example.com/x v1.2.3 h1:aaa=\n",
		})

	running := []exposure.Package{
		{Package: "example.com/x", Version: "v1.2.3", Licence: "MIT"},
		{Package: "example.com/y", Version: "v0.4.0", Licence: "Apache-2.0"},
	}
	same := derive(t, exposure.Checkout{Dir: dir, Resolved: running, CurrentRelease: running}, base, head)
	if len(same.DependencyChanges) != 0 {
		t.Errorf("a manifest line changed over the version already running reads as %v", same.DependencyChanges)
	}

	// Nothing in the manifest names example.com/y at its new version, so the
	// entry names the resolved set instead of a file and a line — and it is a
	// dependency change either way.
	moved := derive(t, exposure.Checkout{
		Dir: dir,
		Resolved: []exposure.Package{
			{Package: "example.com/x", Version: "v1.2.3", Licence: "MIT"},
			{Package: "example.com/y", Version: "v0.5.0", Licence: "Apache-2.0"},
		},
		CurrentRelease: running,
	}, base, head)
	if len(moved.DependencyChanges) != 1 {
		t.Fatalf("the dependency changes are %v, want the one package that moved", moved.DependencyChanges)
	}
	entry := moved.DependencyChanges[0]
	if !strings.Contains(entry, "example.com/y moved from v0.4.0 to v0.5.0") ||
		!strings.Contains(entry, "licence Apache-2.0") {
		t.Errorf("the entry is %q, want the package, both versions and the licence", entry)
	}
	if !strings.Contains(entry, "resolved set") {
		t.Errorf("the entry is %q, want it to say the manifest named nothing", entry)
	}
}

// TestACredentialNameRemovedIsRead: the factor reads a credential name added or
// removed the way it reads one added or removed in code, a change to the file
// that holds it being a diff like any other. Reading a removal as nothing would
// be the absence of evidence read as evidence of safety, which this group does
// not do.
func TestACredentialNameRemovedIsRead(t *testing.T) {
	dir, base, head := repoWith(t,
		map[string]string{"config.go": "package main\n\nvar credential = \"model.openrouter\"\n"},
		map[string]string{"config.go": "package main\n"})

	evidence := derive(t, exposure.Checkout{Dir: dir}, base, head)
	if len(evidence.Credentials) != 1 {
		t.Fatalf("the credentials found are %v, want the removed name", evidence.Credentials)
	}
	if !strings.Contains(evidence.Credentials[0], "a removed") ||
		!strings.Contains(evidence.Credentials[0], "model.openrouter") {
		t.Errorf("the entry is %q, want the removal and the name", evidence.Credentials[0])
	}
}

// TestNoGitAtBaseIsUnavailableAndNeverNothing: an extractor that could not run
// resolves the factor, and a diff that added none of this reads as nothing. The
// two are opposite answers and this is where they are told apart.
func TestNoGitAtBaseIsUnavailableAndNeverNothing(t *testing.T) {
	dir, _, head := repoWith(t,
		map[string]string{"main.go": "package main\n"},
		map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	evidence, coverage, err := exposure.Derive(context.Background(),
		exposure.Checkout{Dir: dir}, "0000000000000000000000000000000000000000", head)
	if err != nil {
		t.Fatalf("a commit that is not there is a reading and not an error: %v", err)
	}
	if evidence.Unavailable == "" {
		t.Fatal("a diff that could not be taken reads as nothing rather than as unavailable")
	}
	if coverage.Unavailable != evidence.Unavailable {
		t.Errorf("the coverage says %q and the evidence says %q", coverage.Unavailable, evidence.Unavailable)
	}
	if len(evidence.List()) != 0 {
		t.Errorf("an unavailable derivation carries the entries %v", evidence.List())
	}

	// A checkout naming no directory is the caller's defect and not a reading.
	if _, _, err := exposure.Derive(context.Background(), exposure.Checkout{}, "a", "b"); err == nil {
		t.Error("a checkout with no directory derived something")
	}
}
