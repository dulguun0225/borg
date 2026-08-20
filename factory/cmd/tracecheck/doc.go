// Command tracecheck fails the build on a reference from the code or its
// documentation into a Markdown file that points at nothing — a target
// whose file or directory does not exist, or a "#anchor" matching no
// heading in the file it names — and on a doc.go that names no target at
// all. It is what checks the "What defines it" line every package's doc.go
// carries: nothing else reads a path inside a Go comment, because the
// consistency pass in ../../../end-goal/CLAUDE.md scopes every command it
// runs to end-goal alone. Run it from factory/.
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
// Three things are an error: a target whose file or directory does not
// exist; where the target names an anchor and the target is Markdown, an
// anchor matching no heading in that file; and a doc.go, anywhere under
// the walk, that contributed no reference at all. A heading is slugged
// the way the consistency pass slugs one — lowercase, every letter,
// digit, space, and hyphen kept, everything else dropped, and each space
// turned to a hyphen — so the two agree; that differs from GitHub, which
// appends "-1" to a repeated heading's second slug, and this check does
// not. The third forces a cost: a doc.go whose concept is defined nowhere
// in end-goal/ now has to say so — "What defines it: nothing yet" or
// similar — rather than stay silent about it.
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
// What defines it: "The map ships with the code" under Code in
// ../../../CLAUDE.md#code.
package main
