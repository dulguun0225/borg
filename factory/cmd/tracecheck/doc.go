// Command tracecheck fails the build on a reference from the code or its
// documentation into a Markdown file that points at nothing — a target
// whose file or directory does not exist, or a "#anchor" matching no
// heading in the file it names — and on a doc.go that names no target at
// all. Run it from factory/.
//
// It walks the working directory and reads every *.go and *.md file under
// it, extracting a reference two ways. A Markdown link — a target in
// parentheses right after a closing bracket — is read wherever it
// appears, skipping a bare "#anchor" and an "http:" or "https:" target. A
// bare relative path is read only where it contains a "/" and ends in
// ".md" — the idiom a Go comment uses in place of link syntax — requiring
// the "/" is what keeps a prose mention of a filename such as "CLAUDE.md"
// from being read as a path. In a .go file both are read from a comment
// line alone, one that is nothing but "//" and text once trimmed, so a
// generic function's type parameter and a "postgres://" URL are never
// read as one; in a .md file every line is read. Each target resolves
// against the directory of the file it was found in.
//
// refs.go is [Reference] with [ExtractFile], which reads the references out
// of one file, [Check], which decides each against the tree, and [Uncited],
// the doc.go that contributed none, with the heading slugs beneath them;
// main.go is the entry point and the walk that collects every *.go and *.md
// file. The tests are refs_test.go, which needs no database.
//
// Three things are an error: a target whose file or directory does not
// exist; where the target names an anchor and the target is Markdown, an
// anchor matching no heading in that file; and a doc.go, anywhere under
// the walk, that contributed no reference at all. A heading is slugged
// the way the consistency pass slugs one — lowercase, every letter,
// digit, space, and hyphen kept, everything else dropped, and each space
// turned to a hyphen — so the two agree; that differs from GitHub, which
// appends "-1" to a repeated heading's second slug, and this check does
// not.
//
// What it does not see: a link or a path split across a line break,
// because extraction reads one line at a time; a scheme other than
// "http:" or "https:", which is read as a relative path and reported
// missing rather than recognized as external; a "#"-prefixed line inside a
// fenced code block, which is counted as a heading the same way the
// consistency pass counts it; a reference in a .go file's trailing
// comment, one that shares a line with code, because only a line that is
// a comment on its own is read; and a same-directory reference with no
// "/" written as a bare path rather than as a Markdown link — the slash
// requirement above exists to exclude exactly that.
//
// Who may write what: this command writes nothing. It opens no database,
// it reads end-goal/ and never edits it — a broken reference is fixed by a
// person, not by the tool that found it — and it reports what it found on
// standard error and in its exit status.
//
// What defines it: "The map ships with the code" under Code in
// ../../../CLAUDE.md#code; the slug rule and the scope of the commands the
// consistency pass runs are ../../../end-goal/CLAUDE.md.
package main
