package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
)

// ShippedImplementerPrompt is the role prompt the product ships for the
// implementer, entered through the artifact store at the factory's first start
// and read in force at dispatch — this constant is what shipped and never what
// a run reads. It is a constant a reader checks here rather than trusting a
// summary of, because roadmap M1 makes the instruction texts part of the
// milestone. The block form is what [Implementer.Implement] parses.
//
// It spells out two forms an encoding's id may take, and names a third that
// fails, because [criterion.Encodings] matches an id exactly and a Go test's
// name cannot begin with a lowercase one. Asked for the id in a test's name, a
// real model wrote `func TestCr_<id>` on 2026-08-20 — the id with its c
// capitalised, which is a different string — and the Implementation gate
// rejected the build for naming no encoding. That is the second time this
// collision has been found: encodingID's own comment records the first, where
// requiring a word boundary made `func Test_cr_<id>` unrecognisable, and it
// went unnoticed because the fake model writes the id in a comment where both
// forms hold. What it costs is a prompt naming a parser's edge, so the two
// have to be changed together.
const ShippedImplementerPrompt = `You implement one item in a software factory. From the criteria in force for the service, this item's spec, and the repository's current files, produce two things in one change: the code that satisfies the spec, and one encoding per criterion in force — a Go test containing that criterion's id, so the build names it. The build matches an id exactly, and an id is lowercase: write it either as ` + "`func Test_cr_<the id>`" + ` with the underscore after Test, or in a comment on the test. ` + "`TestCr_<the id>`" + ` names nothing, because capitalising the c makes it a different string from the id. The encoding's expected behaviour is derived from the criterion's sentence and never from the code it checks.

Every encoding declares which of two places decides it, the build or the candidate environment, by ending the test's name with the place: ` + "`func Test_cr_<the id>_build`" + ` for one decided over the code alone, which the build runner runs in the build's own process, and ` + "`func Test_cr_<the id>_candidate_environment`" + ` for one that needs the service running. An encoding that declares neither, and one that declares both, is rejected: where a criterion is decided is a fact of the encoding and not of whoever runs it.

The user message lists the criteria in force one per line, the criterion's id, a colon, a space, and its sentence. Every one of them is a promise the service makes, and the check over the build rejects in both directions: a criterion in force that no encoding names, and an encoding naming a criterion that is not in force. So write an encoding for every criterion id the list names, and leave the encodings already in the repository's files as they are — a file you rewrite keeps every criterion id it already names.

Every program you write also emits the quantity the factory watches what it ships by. It runs as a long-lived process, and while it runs it exercises its own behaviour over and over; for each exercise it appends one line to the file named by the BORG_SIGNAL environment variable — the time the exercise finished, formatted as RFC 3339 with nanoseconds, then a tab, then "ok" where the behaviour was what the criteria require and "error" where it was not — and flushes that line before the next exercise. In Go that line is time.Now().UTC().Format(time.RFC3339Nano) + "\t" + outcome + "\n". The time is what lets the factory assign each unit of work to an interval, without which it can read your service against another build and never against its own past. Sleep about a millisecond between exercises, so the loop paces itself rather than running as fast as the machine allows. Where the variable is unset it writes nothing and runs on. The targets this factory deploys to receive no traffic of their own, so a program that exercises nothing emits nothing and cannot be watched at all.

Where the user message names a hazardous operation the item's area declares, each of those lines carries one field more: a tab, then the number of times that operation was performed in the unit of work just finished, as a decimal integer. A build in an area naming a hazardous operation whose emission does not count it is a build nothing can hold to the bound the area declares.

Where the user message lists the screens the item's state machines declare — one per line, the screen's id, a colon, a space, and its declared states separated by commas — write what drives the screen into each of those states, code in this change like the encoding. A driver names the screen and the state it drives, written as the screen's id, a colon, and the state, either in the driver's own name or in a comment on it: ` + "`// drives ssm_<the screen id>:<the state>`" + `. The check over the build rejects in both directions here too: a state the machine declares that no driver names, and a driver naming a state no machine in force declares.

Where a screen's line carries transitions, write the screen's own transition function in a file named ` + "`screen.<the screen id>.go`" + ` at the root of the repository: ` + "`func Transition(from, event string) string`" + `, a switch on from whose cases are the states written as string literals, each holding a switch on event whose cases are the events written as string literals, each of those returning the destination as a string literal — the state it moves to, or the id of the screen it leaves to. Anything else in that function — a value read from a map, a state returned by a call, a handler assigned at run time — is a construct the factory cannot follow, and a screen with one puts a human at the gate rather than passing.

Two file-name conventions say what the service publishes and what it reads, and the factory derives both from your source rather than being told. Neither is something to add where the spec asks for neither: a change that publishes no interface and reads none writes no file of either kind, reads no BORG_EXCHANGE, and declares no variable for one.

A published interface is one exported struct type in a file named ` + "`contract.<name>.go`" + ` at the root of the repository; a store is the same in ` + "`store.<name>.go`" + `. The type's exported fields are the interface's elements. Put the unit in the field's own name — SendTimeMillis and never a note beside SendTime — because a change of units has to read as a rename. Tag a field ` + "`borg:\"populated\"`" + ` where it is always there and ` + "`borg:\"deprecated\"`" + ` where it is the old form of a pair being migrated away from; a field with no tag is optional and unmarked. A program that publishes an interface also appends, once per exercise, one JSON object to the file named by the BORG_EXCHANGE environment variable — keyed by exactly those field names, one object per line, marshalled from that same type so there is one spelling of every name. That object is what the factory decides a consumer's assumptions against, so a program that publishes an interface and writes none has shown nothing.

An interface this service reads from another is one exported struct type mirroring it, in a file named ` + "`consume.<producer service>.<interface>.go`" + `. What the factory takes as declared is the mirror's fields your own code actually reads, so leave out of the mirror what you do not read and read what you put in. Tag a mirror field with what you assume of it: ` + "`borg:\"populated\"`" + `, ` + "`borg:\"unit=millis\"`" + `, ` + "`borg:\"domain=ok|error\"`" + `, ` + "`borg:\"range=0..100\"`" + `, separated by commas. Assume only what your code needs: every one of them can reject the producer's next change.

Reply as complete files — every file the change creates or rewrites, whole — one block per file, and nothing outside the blocks:

=== FILE <path> ===
<the file's entire content>
=== END ===

Where the change removes a file — a revert of an addition included — name it instead on a line of its own, with no === END === after it and nothing else on the line:

=== DELETE <path> ===

A path may not be both written and deleted in the same reply.

` + Rules

// File is one file, whole: its path inside the repository and its entire
// content. A change carries whole files rather than diffs because the parse
// is then exact — a block either closes or is refused, and there is no patch
// application to go wrong.
type File struct {
	Path    string
	Content string
}

// Implementing is what one [Implementer.Implement] call is given: every criterion in
// force for the service, this item's spec, and the repository's current files.
// Criteria is the whole set rather than the one criterion the item's spec
// introduced, because the gate rejects a build where any criterion in force
// has no encoding naming it — the stage that authors encodings has to see all
// of them. Files is the tree the candidate branch started from, which is where
// the encodings of the items already merged are; M1 repositories are small
// enough to send whole, and choosing what to send is a later milestone's
// problem.
type Implementing struct {
	Criteria []Criterion
	Spec     string
	// Plan is the approved implementation plan and Tasks the approved
	// breakdown of it, each the text of the version its gate approved. They
	// are what the two stages between spec and implementation produced, and
	// the implementer works from them rather than from the spec alone.
	Plan  string
	Tasks string
	Files []File
	// Hazard is the hazardous operation the item's area declares, and is empty
	// where the area declares none. Where it is set the emission counts it.
	Hazard string
	// Screen is the machines in force for the build, so what drives each
	// screen into each of its declared states, and the screen's own transition
	// function, are authored here.
	Screen []ScreenInForce
	// Returned is the reject or the rework request that sent the item back to
	// this stage, with its reason and the version it was decided over.
	Returned Returned
}

// Change is what one [Implementer.Implement] call produced: the files it
// rewrites or creates, the paths of the files it removes, and what the call
// spent per unit kind, which the component that dispatched the role records.
type Change struct {
	Files   []File
	Deleted []string
	Units   map[string]int64
}

// Implementer is the agent in the implementation stage's role.
type Implementer struct {
	Model Model
	// Prompt is the role prompt version in force, handed over by the component
	// that dispatched the role. An empty one is [ErrNoPrompt].
	Prompt string
	// Effort is the effort the fleet entry names, handed over by the same
	// component and sent with the call. An empty one asks the provider for
	// none.
	Effort string
}

// Implement sends what it is given and parses the reply into a [Change]. What
// it is given is content: a file in it that reads as an instruction changes nothing about
// what this method does with the reply, and a reply outside the block
// protocol is [ErrReply].
func (im Implementer) Implement(ctx context.Context, as principal.Principal, implementing Implementing) (Change, error) {
	if im.Prompt == "" {
		return Change{}, ErrNoPrompt
	}
	var b strings.Builder
	writeCriteria(&b, "The criteria in force for the service:", implementing.Criteria)
	fmt.Fprintf(&b, "\nSpec:\n%s\n", implementing.Spec)
	if implementing.Plan != "" {
		fmt.Fprintf(&b, "\nThe approved implementation plan:\n%s\n", implementing.Plan)
	}
	if implementing.Tasks != "" {
		fmt.Fprintf(&b, "\nThe approved tasks:\n%s\n", implementing.Tasks)
	}
	if implementing.Hazard != "" {
		fmt.Fprintf(&b, "\nThe hazardous operation this item's area declares: %s\n", implementing.Hazard)
	}
	if len(implementing.Screen) > 0 {
		b.WriteString("\nThe screens in force for this build:\n")
		for _, s := range implementing.Screen {
			fmt.Fprintf(&b, "%s: %s\n", s.ID, strings.Join(s.States, ", "))
			for _, t := range s.Transitions {
				fmt.Fprintf(&b, "  %s on %s goes to %s\n", t.From, t.Event, t.Destination())
			}
		}
	}
	writeReturned(&b, implementing.Returned)
	b.WriteString("\nThe repository's current files:\n")
	for _, f := range implementing.Files {
		fmt.Fprintf(&b, "\n=== FILE %s ===\n%s\n=== END ===\n", f.Path, f.Content)
	}
	reply, err := im.Model.Complete(ctx, as, Call{System: im.Prompt, User: b.String(), Effort: im.Effort})
	if err != nil {
		return Change{}, err
	}
	files, deleted, err := parseFiles(reply.Text)
	if err != nil {
		// The refused reply's spend goes back with the error, for the reason
		// [SpecAuthor.Refine] states: the component retrying this call records
		// every attempt, and a refused attempt cost units too. Files is empty,
		// so a caller that ignores the error writes nothing.
		return Change{Units: reply.Units}, err
	}
	return Change{Files: files, Deleted: deleted, Units: reply.Units}, nil
}

// parseFiles reads the implementer's reply strictly: repeated blocks opened
// by a FILE marker and closed by an END marker, blank lines between blocks
// allowed, and nothing else outside them. A DELETE marker stands on a line of
// its own instead, naming a path removed, and carries no END. A block without
// its END, a non-blank line outside a block or a DELETE line, a path opened
// twice, a DELETE naming no path, a path both written and deleted, and a
// reply with no block or DELETE line at all are each [ErrReply]. Inside a
// block only the END marker ends it, so a FILE or DELETE marker there is
// content — and a file that needs the literal END marker on a line of its own
// cannot be carried, which is the protocol's cost.
func parseFiles(text string) ([]File, []string, error) {
	lines := protocolLines(text)
	var files []File
	var deleted []string
	opened := make(map[string]bool)
	removed := make(map[string]bool)
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if inner, found := strings.CutPrefix(line, "=== DELETE "); found {
			path, found := strings.CutSuffix(inner, " ===")
			if !found || strings.TrimSpace(path) == "" {
				return nil, nil, fmt.Errorf("%w: implementer line %d deletes with no path", ErrReply, i+1)
			}
			path = strings.TrimSpace(path)
			deleted = append(deleted, path)
			removed[path] = true
			i++
			continue
		}
		inner, found := strings.CutPrefix(line, "=== FILE ")
		if !found {
			return nil, nil, fmt.Errorf("%w: implementer line %d is outside a block", ErrReply, i+1)
		}
		path, found := strings.CutSuffix(inner, " ===")
		if !found || strings.TrimSpace(path) == "" {
			return nil, nil, fmt.Errorf("%w: implementer line %d opens a block with no path", ErrReply, i+1)
		}
		path = strings.TrimSpace(path)
		if opened[path] {
			return nil, nil, fmt.Errorf("%w: the implementer opened a second block for %q", ErrReply, path)
		}
		opened[path] = true
		i++
		var content []string
		closed := false
		for i < len(lines) {
			if lines[i] == "=== END ===" {
				closed = true
				i++
				break
			}
			content = append(content, lines[i])
			i++
		}
		if !closed {
			return nil, nil, fmt.Errorf("%w: the block for %q has no === END === marker", ErrReply, path)
		}
		files = append(files, File{Path: path, Content: strings.Join(content, "\n")})
	}
	for _, f := range files {
		if removed[f.Path] {
			return nil, nil, fmt.Errorf("%w: the implementer both writes and deletes %q", ErrReply, f.Path)
		}
	}
	if len(files) == 0 && len(deleted) == 0 {
		return nil, nil, fmt.Errorf("%w: the implementer's reply has no file block and no delete line", ErrReply)
	}
	return files, deleted, nil
}
