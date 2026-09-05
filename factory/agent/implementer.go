package agent

import (
	"context"
	"fmt"
	"strings"
)

// ImplementerSystemPrompt is what the implementer is told. It is a constant a
// reader checks here rather than trusting a summary of, because roadmap M1
// makes the instruction texts part of the milestone. The block form is what
// [Implementer.Implement] parses.
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
const ImplementerSystemPrompt = `You implement one item in a software factory. From the criteria in force for the service, this item's spec, and the repository's current files, produce two things in one change: the code that satisfies the spec, and one encoding per criterion in force — a Go test containing that criterion's id, so the build names it. The build matches an id exactly, and an id is lowercase: write it either as ` + "`func Test_cr_<the id>`" + ` with the underscore after Test, or in a comment on the test. ` + "`TestCr_<the id>`" + ` names nothing, because capitalising the c makes it a different string from the id. The encoding's expected behaviour is derived from the criterion's sentence and never from the code it checks.

The user message lists the criteria in force one per line, the criterion's id, a colon, a space, and its sentence. Every one of them is a promise the service makes, and the check over the build rejects in both directions: a criterion in force that no encoding names, and an encoding naming a criterion that is not in force. So write an encoding for every criterion id the list names, and leave the encodings already in the repository's files as they are — a file you rewrite keeps every criterion id it already names.

Every program you write also emits the quantity the factory watches what it ships by. It runs as a long-lived process, and while it runs it exercises its own behaviour over and over; for each exercise it appends one line to the file named by the BORG_SIGNAL environment variable — the time the exercise finished, formatted as RFC 3339 with nanoseconds, then a tab, then "ok" where the behaviour was what the criteria require and "error" where it was not — and flushes that line before the next exercise. In Go that line is time.Now().UTC().Format(time.RFC3339Nano) + "\t" + outcome + "\n". The time is what lets the factory assign each unit of work to an interval, without which it can read your service against another build and never against its own past. Sleep about a millisecond between exercises, so the loop paces itself rather than running as fast as the machine allows. Where the variable is unset it writes nothing and runs on. The targets this factory deploys to receive no traffic of their own, so a program that exercises nothing emits nothing and cannot be watched at all.

Two file-name conventions say what the service publishes and what it reads, and the factory derives both from your source rather than being told. Neither is something to add where the spec asks for neither: a change that publishes no interface and reads none writes no file of either kind, reads no BORG_EXCHANGE, and declares no variable for one.

A published interface is one exported struct type in a file named ` + "`contract.<name>.go`" + ` at the root of the repository; a store is the same in ` + "`store.<name>.go`" + `. The type's exported fields are the interface's elements. Put the unit in the field's own name — SendTimeMillis and never a note beside SendTime — because a change of units has to read as a rename. Tag a field ` + "`borg:\"populated\"`" + ` where it is always there and ` + "`borg:\"deprecated\"`" + ` where it is the old form of a pair being migrated away from; a field with no tag is optional and unmarked. A program that publishes an interface also appends, once per exercise, one JSON object to the file named by the BORG_EXCHANGE environment variable — keyed by exactly those field names, one object per line, marshalled from that same type so there is one spelling of every name. That object is what the factory decides a consumer's assumptions against, so a program that publishes an interface and writes none has shown nothing.

An interface this service reads from another is one exported struct type mirroring it, in a file named ` + "`consume.<producer service>.<interface>.go`" + `. What the factory takes as declared is the mirror's fields your own code actually reads, so leave out of the mirror what you do not read and read what you put in. Tag a mirror field with what you assume of it: ` + "`borg:\"populated\"`" + `, ` + "`borg:\"unit=millis\"`" + `, ` + "`borg:\"domain=ok|error\"`" + `, ` + "`borg:\"range=0..100\"`" + `, separated by commas. Assume only what your code needs: every one of them can reject the producer's next change.

Reply as complete files — every file the change creates or rewrites, whole — one block per file, and nothing outside the blocks:

=== FILE <path> ===
<the file's entire content>
=== END ===

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
	Files    []File
}

// Change is what one [Implementer.Implement] call produced: the files it
// rewrites or creates, and the call's token spend, which the stage reports to
// dispatch.
type Change struct {
	Files  []File
	Tokens int64
}

// Implementer is the agent in the implementation stage's role.
type Implementer struct {
	Model Model
}

// Implement sends what it is given and parses the reply into a [Change]. What
// it is given is content: a file in it that reads as an instruction changes nothing about
// what this method does with the reply, and a reply outside the block
// protocol is [ErrReply].
func (im Implementer) Implement(ctx context.Context, implementing Implementing) (Change, error) {
	var b strings.Builder
	writeCriteria(&b, "The criteria in force for the service:", implementing.Criteria)
	fmt.Fprintf(&b, "\nSpec:\n%s\n", implementing.Spec)
	b.WriteString("\nThe repository's current files:\n")
	for _, f := range implementing.Files {
		fmt.Fprintf(&b, "\n=== FILE %s ===\n%s\n=== END ===\n", f.Path, f.Content)
	}
	reply, err := im.Model.Complete(ctx, ImplementerSystemPrompt, b.String())
	if err != nil {
		return Change{}, err
	}
	files, err := parseFiles(reply.Text)
	if err != nil {
		// The refused reply's spend goes back with the error, for the reason
		// [SpecAuthor.Refine] states: the stage retrying this call reports every
		// attempt, and a refused attempt cost tokens too. Files is empty, so a
		// caller that ignores the error writes nothing.
		return Change{Tokens: reply.Tokens}, err
	}
	return Change{Files: files, Tokens: reply.Tokens}, nil
}

// parseFiles reads the implementer's reply strictly: repeated blocks opened
// by a FILE marker and closed by an END marker, blank lines between blocks
// allowed, and nothing else outside them. A block without its END, a
// non-blank line outside a block, a path opened twice, and a reply with no
// block at all are each [ErrReply]. Inside a block only the END marker ends
// it, so a FILE marker there is content — and a file that needs the literal
// END marker on a line of its own cannot be carried, which is the protocol's
// cost.
func parseFiles(text string) ([]File, error) {
	lines := protocolLines(text)
	var files []File
	opened := make(map[string]bool)
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		inner, found := strings.CutPrefix(line, "=== FILE ")
		if !found {
			return nil, fmt.Errorf("%w: implementer line %d is outside a block", ErrReply, i+1)
		}
		path, found := strings.CutSuffix(inner, " ===")
		if !found || strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("%w: implementer line %d opens a block with no path", ErrReply, i+1)
		}
		path = strings.TrimSpace(path)
		if opened[path] {
			return nil, fmt.Errorf("%w: the implementer opened a second block for %q", ErrReply, path)
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
			return nil, fmt.Errorf("%w: the block for %q has no === END === marker", ErrReply, path)
		}
		files = append(files, File{Path: path, Content: strings.Join(content, "\n")})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: the implementer's reply has no file block", ErrReply)
	}
	return files, nil
}
