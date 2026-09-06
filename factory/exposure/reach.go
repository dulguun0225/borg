package exposure

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// OutboundPackages is what a Go build reaches the world through: the packages
// whose use is a call leaving the process. A new import of one of them, or a new
// call into one, is an outbound call added.
//
// It is a list and not a rule about paths because the list is what a human
// argues with. A package outside it is one this extractor does not read as
// outbound, and adding one is a line here.
var OutboundPackages = []string{
	"net/http", "net", "net/smtp", "os/exec", "database/sql", "google.golang.org/grpc",
}

// CredentialWords are what a name has to carry for a read of it to be a
// credential read. They are matched against the upper-cased name, so a lowercase
// spelling matches the same word.
var CredentialWords = []string{"KEY", "TOKEN", "SECRET", "PASSWORD"}

// AuthorizationWords are what a function's name has to carry for a removed call
// to it to be an authorization check removed. A check named outside them is one
// this extractor does not read, which is the cost of a list a human can argue
// with.
var AuthorizationWords = []string{"Authorize", "Authenticate", "Permit", "CheckAccess"}

// httpClientMethods are the methods of an http client, a call to one of which is
// an outbound call however the client was obtained. The receiver has to read as
// an http client — its name carrying "client" or "http" — because these are
// method names ordinary code uses for its own types, and an extractor that read
// every Get as an outbound call would put a human at every gate. What it costs
// is a client named neither, whose calls this does not read.
var httpClientMethods = []string{"Do", "Get", "Head", "Post", "PostForm"}

// secretName is the shape a secret's name takes in this factory: lowercase
// segments joined by dots, as "deploy.local" and "model.openrouter" are written.
// A string literal that is entirely one of those is a credential named, which is
// what a build hard-coding a reference looks like. The whole literal has to
// match, so a sentence carrying a dotted word is not one.
var secretName = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z0-9_-]+)+$`)

// literal is a Go string literal on one line, matched for its content alone.
var literal = regexp.MustCompile(`"([^"\\]*)"`)

// call is an identifier or a selector followed by an open bracket, which is what
// a call reads as on one line.
var call = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)\(|\b([A-Z][A-Za-z0-9_]*)\(`)

// getenv is a read of one environment variable by a literal name.
var getenv = regexp.MustCompile(`Getenv\("([^"]*)"\)`)

// outboundCall is the entry an added line makes as an outbound call added, and
// false where it makes none.
func outboundCall(c change) (string, bool) {
	if c.Removed {
		return "", false
	}
	for _, pkg := range OutboundPackages {
		if strings.Contains(c.Text, `"`+pkg+`"`) {
			return entry(c, "a new import of "+pkg), true
		}
	}
	for _, match := range call.FindAllStringSubmatch(c.Text, -1) {
		receiver, method := match[1], match[2]
		if receiver == "" {
			continue
		}
		for _, pkg := range OutboundPackages {
			if receiver == path.Base(pkg) {
				return entry(c, fmt.Sprintf("a new call to %s.%s", receiver, method)), true
			}
		}
		lowered := strings.ToLower(receiver)
		if !strings.Contains(lowered, "client") && !strings.Contains(lowered, "http") {
			continue
		}
		for _, name := range httpClientMethods {
			if method == name {
				return entry(c, fmt.Sprintf("a new call to the http client method %s.%s", receiver, method)), true
			}
		}
	}
	return "", false
}

// credential is the entry a changed line makes as a credential named or read.
//
// A removed line makes one as an added line does: the factor reads a credential
// name added or removed the way it reads one added or removed in code, a change
// to the file that holds it being a diff like any other. What a removal reaches
// that the service did not reach before is the name itself, which is on the list
// beside the diff for the human at Implementation to read either way — and the
// group only ever raises the number, so reading a removal as nothing would be
// the absence of evidence read as evidence of safety.
func credential(c change) (string, bool) {
	if strings.Contains(c.Text, "secretref.Resolve(") {
		return entry(c, direction(c)+" call to secretref.Resolve"), true
	}
	for _, match := range getenv.FindAllStringSubmatch(c.Text, -1) {
		if word, carried := credentialWord(match[1]); carried {
			return entry(c, fmt.Sprintf("%s read of the environment variable %s, whose name carries %s",
				direction(c), match[1], word)), true
		}
	}
	for _, match := range literal.FindAllStringSubmatch(c.Text, -1) {
		if secretName.MatchString(match[1]) {
			return entry(c, direction(c)+" string literal shaped like a secret's name: "+match[1]), true
		}
	}
	return "", false
}

// direction is which side of the diff a line is on, in the words an entry is
// read in.
func direction(c change) string {
	if c.Removed {
		return "a removed"
	}
	return "a new"
}

// credentialWord is the word a name carries that makes reading it a credential
// read, and false for a name carrying none.
func credentialWord(name string) (string, bool) {
	upper := strings.ToUpper(name)
	for _, word := range CredentialWords {
		if strings.Contains(upper, word) {
			return word, true
		}
	}
	return "", false
}

// authorizationCheck is the entry a removed line makes as an authorization check
// removed or weakened: a removed call to a function whose name carries one of
// [AuthorizationWords], and a removed if guard around one, which is the same
// line read as the weakening rather than the removal.
func authorizationCheck(c change) (string, bool) {
	if !c.Removed {
		return "", false
	}
	guard := strings.HasPrefix(strings.TrimSpace(c.Text), "if ")
	for _, match := range call.FindAllStringSubmatch(c.Text, -1) {
		name := match[2]
		if name == "" {
			name = match[3]
		}
		for _, word := range AuthorizationWords {
			if !strings.Contains(name, word) {
				continue
			}
			if guard {
				return entry(c, "a removed if guard on "+name), true
			}
			return entry(c, "a removed call to "+name), true
		}
	}
	return "", false
}

// dependencyChanges is the build's own resolved set diffed against the set of
// the service's current release's build: each package added or moved, named with
// its version and its declared licence. It is a diff of two sets and not a read
// of the changed lines of go.mod and go.sum, because what a build resolved is
// what it runs — an unpinned range that resolves differently with nothing in the
// manifest changed is read as the change it is, and a manifest line changed that
// resolved to the version already running is not one.
//
// A package removed is not here. The group only ever raises the number and what
// it names is what the change reaches that the service did not reach before, so
// a package the build no longer resolves reaches nothing new.
//
// A current release's set with nothing in it is a service with no current
// release, which is the reading a first release already gets everywhere else
// here: every package in the build is one the service did not reach before.
func dependencyChanges(resolved, currentRelease []Package, changes []change) []string {
	was := map[string]string{}
	for _, p := range currentRelease {
		was[p.Package] = p.Version
	}
	var found []string
	seen := map[string]bool{}
	for _, p := range resolved {
		before, ran := was[p.Package]
		if (ran && before == p.Version) || seen[p.Package] {
			continue
		}
		seen[p.Package] = true
		what := fmt.Sprintf("%s %s, %s", p.Package, p.Version, licenceOf(p))
		if ran {
			what = fmt.Sprintf("%s moved from %s to %s, %s", p.Package, before, p.Version, licenceOf(p))
		}
		found = append(found, manifestLine(p.Package, changes)+" — "+what)
	}
	return found
}

// licenceOf is the licence the build's resolved set declares for one package,
// and what the entry says where the set declares none.
func licenceOf(p Package) string {
	if p.Licence == "" {
		return "a licence the build's resolved set does not name"
	}
	return "licence " + p.Licence
}

// manifestLine is where a package's change appears in the manifest, so the entry
// names the file and the line the way every other one does. Where nothing in the
// manifest names it, the entry says so: that is the unpinned range that resolved
// differently with nothing changed, and it is a dependency change with no line
// to point at rather than no dependency change.
func manifestLine(pkg string, changes []change) string {
	for _, c := range changes {
		name := path.Base(c.File)
		if name != "go.mod" && name != "go.sum" {
			continue
		}
		if named, _, ok := packageVersion(c.Text); ok && named == pkg {
			return fmt.Sprintf("%s:%d", c.File, c.Line)
		}
	}
	return "the build's resolved set, with nothing in the manifest changed"
}

// packageVersion is the package and the version one line of go.mod or go.sum
// names, and false for a line naming neither. A version is a field beginning
// with "v" and a digit, and the package is the field before it.
func packageVersion(text string) (pkg, version string, ok bool) {
	fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(text), "// indirect"))
	for i, field := range fields {
		if i == 0 || len(field) < 2 || field[0] != 'v' || field[1] < '0' || field[1] > '9' {
			continue
		}
		return fields[i-1], strings.TrimSuffix(field, "/go.mod"), true
	}
	return "", "", false
}

// entry is one line of the evidence list: where it is, and what was found there.
// The file and the line are first because the list is read beside the diff.
func entry(c change, what string) string {
	return fmt.Sprintf("%s:%d — %s", c.File, c.Line, what)
}
