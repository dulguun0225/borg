// Command tracecheck fails the build where the code and the design do not
// hold to each other. It holds four things to one another: end-goal/claims.txt
// as an inventory of claims about the design, each traced to a sentence
// in the file it names; the code's citation of a claim, from a doc.go
// outside cmd/ or a screen README; a reference from the code or its
// documentation into a Markdown file, which must resolve; and a package's
// name, which must be a phrase the design uses. Run it from factory/.
//
// What fails the build: a claims.txt line with other than four
// tab-separated fields; a claim's id that is not C and four digits; a
// claim's id repeating an earlier line; a claim's file not ending in .md
// or escaping end-goal/ with a ".." element; a claim's status other than
// built, stated, or "unbuilt Mn"; a claim's sentence empty; a claim
// naming a file that does not exist; a claim's file, or a reference's
// target, that cannot be read for a reason other than absence, reported
// with the error; a claim's sentence not found, or found more than
// once, in the file it names; a claim's unbuilt milestone naming no
// "## Mn" heading in roadmap.md; a citation naming an id no claim has; a
// built claim nothing cites; an unbuilt or stated claim something
// cites; a non-command doc.go or a screen README citing no claim; a
// design file under how-the-factory-works/ or what-the-factory-does/,
// other than a README, that no claim names; a command's doc.go carrying
// no reference at all; a reference resolving to a target that does not
// exist; a target naming an anchor matching no heading in the file it
// names; one of the four screen directories with no README.md; and a
// package named for no phrase the design uses. The command exits with
// the error itself, rather than a finding, where ../end-goal/claims.txt
// or the roadmap cannot be read at all.
//
// claims.go is [Claim], [ParseClaims], which reads claims.txt, [CheckClaims],
// which holds each claim's sentence and milestone to the tree, and
// [CheckCoverage], which finds a design file under how-the-factory-works/
// or what-the-factory-does/ that no claim names; its test is
// claims_test.go. citations.go is [Citation], [ExtractCitations], which
// reads a claim id out of a doc.go or a screen README, [CheckCitations],
// which holds citation and claim to each other in both directions, and
// [Unclaimed], the doc.go or screen README that cited none; its test is
// citations_test.go. names.go is [PackageDirs], the package directories
// under a root, [DesignPhrases], every one-, two-, and three-word run the
// design's Markdown holds, and [CheckPackageNames], which holds a
// package's name to one; its test is names_test.go. refs.go is
// [Reference] with [ExtractFile], which reads the references out of one
// file, [Check], which decides each against the tree, and [Uncited],
// every doc.go and every screen README that contributed no reference at
// all — main keeps only the command ones from what it returns, a
// non-command doc.go or a screen README being held to [Unclaimed]
// instead; its test is refs_test.go.
// client.go is [isScreenReadme], which [Uncited] and [ExtractCitations]
// use to tell a screen README from any other, and
// [MissingScreenReadmes], which finds a screen directory with no
// README.md at all; its test is client_test.go. main.go is the entry
// point, the walk that collects every *.go and *.md file, and the wiring
// that runs every check in turn; it has no test of its own.
//
// A Markdown link — a target in parentheses right after a closing bracket
// — is read wherever it appears, skipping a bare "#anchor" and an "http:"
// or "https:" target; its text, where the link's opening bracket is on
// the same line, is covered along with the target and is not read a
// second time as a bare path, except that a "]" nested inside the text
// ends the covered span there, so a numbered name written after a nested
// bracket is read again. A bare relative path is read only where it ends
// in ".md" — the idiom a Go comment uses in place of link syntax — and
// either contains a "/", begins with two digits and a hyphen (the
// design's file naming), or is the base name of a .md file end-goal/
// holds anywhere beneath it — a set built once, by walking end-goal/,
// before extraction runs — so a sibling named without its path, and a
// design file mentioned by its bare name alone, are both read rather
// than skipped. Two names are read as prose regardless: "README.md" and
// "CLAUDE.md", because both exist across the repository and so name no
// one file. A name matching none of this — a prose mention of an
// ordinary filename — is not read as a path. In a .go file
// both a reference and a claim citation are read from a comment line
// alone, one that is nothing but "//" and text once trimmed, so a
// generic function's type parameter and a "postgres://" URL are never
// read as either; in a .md file every line is read for both. Each
// reference resolves against the directory of the file it was found in;
// a citation is any "Cnnnn" the line holds, read from a doc.go outside
// cmd/ or a screen README alone — the two kinds of file this command
// holds to the claims inventory. A heading is slugged the way the
// consistency pass slugs one — lowercase, every letter, digit, space,
// and hyphen kept, everything else dropped, and each space turned to a
// hyphen — so the two agree; that differs from GitHub, which appends
// "-1" to a repeated heading's second slug, and this check does not.
//
// What it does not see: a bare path split across a line break, because
// extraction reads one line at a time — a link split the same way is
// still read, but only from the half holding its closing bracket and
// target, its text on the earlier line being invisible to it; a scheme
// other than "http:" or "https:", which is read as a relative path and
// reported missing rather than recognized as external; a "#"-prefixed
// line inside a fenced code block, which is counted as a heading the
// same way the consistency pass counts it; a reference or a citation in
// a .go file's trailing comment, one that shares a line with code,
// because only a line that is a comment on its own is read; and a bare
// same-directory name that is neither numbered, slashed, nor the base
// name of a file end-goal/ holds — or is "README.md" or "CLAUDE.md",
// which are never read as a path even where end-goal/ holds a file of
// that name too — written as a bare path rather than as a Markdown link.
//
// Who may write what: this command writes nothing. It opens no database,
// it reads end-goal/ and claims.txt and never edits either — a broken
// reference or a stale claim is fixed by a person, not by the tool that
// found it — and it reports what it found on standard error and in its
// exit status.
//
// What defines it: "The map ships with the code" under Code in
// ../../../CLAUDE.md#code; the slug rule and the scope of the commands the
// consistency pass runs are ../../../end-goal/CLAUDE.md.
package main
