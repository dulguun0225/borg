// Package clientdist is the directory the Angular build's outputPath points
// at: browser/, embedded with the standard library's embed so one binary
// carries exactly the client built beside it.
//
// It holds no code beyond the embed in embed.go. browser/ is not committed —
// the client's build writes into it, and ../../.gitignore excludes
// everything under it but the one file that makes an otherwise empty
// directory embed on a fresh clone, browser/.gitkeep. A clone that has not
// run the client's build yet embeds an empty directory, which is what
// package screens answers with the 503 naming factory/client.
//
// What defines it:
// ../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md
// (C2695).
package clientdist
