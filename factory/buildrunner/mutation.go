package buildrunner

import (
	"bufio"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dulguun0225/borg/factory/wayin"
)

// MutantRequest bounds the transient artifacts compiled from one checkout.
// No build writer is part of this operation.
type MutantRequest struct {
	Checkout             Checkout
	Cap                  int
	OutputDirectory      string
	RepositoryCredential string
	RegistryCredentials  []string
}

// MutantArtifact is one altered candidate artifact. CompileError is set when
// the bounded alteration was caught by compilation, so the deployer can count
// it without placing anything on the target.
type MutantArtifact struct {
	ArtifactPath string
	File         string
	Line         int
	Operator     string
	CompileError string
}

// CompileMutants compiles bounded mutations of the lines added by the
// checkout's diff. Its operators are deliberately four and only four:
// negate a condition, flip a comparison, replace a literal, and drop a
// statement. Test files and import lines are excluded. It writes artifacts to
// the caller's temporary directory and writes no build record.
func (r *Runner) CompileMutants(ctx context.Context, req MutantRequest) ([]MutantArtifact, error) {
	if req.Checkout.Directory == "" {
		return nil, fmt.Errorf("buildrunner: mutant compilation needs a checkout")
	}
	if req.Cap <= 0 {
		return nil, fmt.Errorf("buildrunner: mutant cap must be positive")
	}
	if r.resolver == nil || r.process == nil {
		return nil, fmt.Errorf("buildrunner: mutant compilation needs resolver and process seams")
	}
	if req.OutputDirectory == "" {
		return nil, fmt.Errorf("buildrunner: mutant compilation needs an output directory")
	}
	if err := os.MkdirAll(req.OutputDirectory, 0o755); err != nil {
		return nil, fmt.Errorf("buildrunner: making mutant output directory: %w", err)
	}
	sites, err := changedSourceLines(ctx, req.Checkout)
	if err != nil {
		return nil, err
	}
	var artifacts []MutantArtifact
	for _, site := range sites {
		if len(artifacts) >= req.Cap {
			break
		}
		source, err := os.ReadFile(filepath.Join(req.Checkout.Directory, site.path))
		if err != nil {
			return nil, fmt.Errorf("buildrunner: reading mutant source %s: %w", site.path, err)
		}
		mutations := mutationsAt(string(source), site.path, site.line)
		for _, mutation := range mutations {
			if len(artifacts) >= req.Cap {
				break
			}
			artifact := MutantArtifact{File: site.path, Line: site.line, Operator: mutation.operator}
			copyDir, err := copyCheckout(req.Checkout.Directory)
			if err != nil {
				return nil, err
			}
			mutated := filepath.Join(copyDir, site.path)
			if err := os.WriteFile(mutated, []byte(mutation.source), 0o644); err != nil {
				_ = os.RemoveAll(copyDir)
				return nil, fmt.Errorf("buildrunner: writing mutant source %s: %w", site.path, err)
			}
			output := filepath.Join(req.OutputDirectory, fmt.Sprintf("mutant-%03d", len(artifacts)+1))
			resolution, resolveErr := r.resolver.Resolve(ctx, Checkout{Directory: copyDir, Base: req.Checkout.Base, Commit: req.Checkout.Commit},
				req.RepositoryCredential, req.RegistryCredentials)
			if resolveErr == nil && resolution.CouldNotDerive == "" {
				overlay, overlayErr := wayin.Overlay(copyDir, req.OutputDirectory, r.shippedBundle)
				if overlayErr != nil {
					resolveErr = overlayErr
				} else {
					_, processErr := r.process.Run(ctx, ProcessInput{
						Checkout:   Checkout{Directory: copyDir, Base: req.Checkout.Base, Commit: req.Checkout.Commit},
						Resolution: resolution, Overlay: overlay, Output: output,
					})
					if processErr == nil {
						artifact.ArtifactPath = output
					} else {
						resolveErr = processErr
					}
				}
			} else if resolveErr == nil {
				resolveErr = fmt.Errorf("buildrunner: mutant resolution could not derive: %s", resolution.CouldNotDerive)
			}
			if resolveErr != nil {
				artifact.CompileError = resolveErr.Error()
			}
			artifacts = append(artifacts, artifact)
			_ = os.RemoveAll(copyDir)
		}
	}
	return artifacts, nil
}

type sourceSite struct {
	path string
	line int
}

type sourceMutation struct {
	operator string
	source   string
}

func changedSourceLines(ctx context.Context, checkout Checkout) ([]sourceSite, error) {
	base := checkout.Base
	if base == "" {
		base = EmptyTree
	}
	commit := checkout.Commit
	if commit == "" {
		commit = "HEAD"
	}
	cmd := execCommand(ctx, checkout.Directory, "git", "diff", "--unified=0", "--no-color", base, commit, "--", "*.go")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("buildrunner: reading the Go lines touched by the diff: %w", err)
	}
	return parseChangedSourceLines(string(output)), nil
}

func parseChangedSourceLines(diff string) []sourceSite {
	var path string
	line, remaining := 0, 0
	var sites []sourceSite
	scanner := bufio.NewScanner(strings.NewReader(diff))
	for scanner.Scan() {
		text := scanner.Text()
		switch {
		case strings.HasPrefix(text, "+++ b/"):
			path = strings.TrimPrefix(text, "+++ b/")
		case strings.HasPrefix(text, "@@ "):
			fields := strings.Fields(text)
			if len(fields) < 3 {
				path, remaining = "", 0
				continue
			}
			newPart := strings.TrimPrefix(fields[2], "+")
			parts := strings.SplitN(newPart, ",", 2)
			line, _ = strconv.Atoi(parts[0])
			remaining = 1
			if len(parts) == 2 {
				remaining, _ = strconv.Atoi(parts[1])
			}
		case path != "" && remaining > 0 && strings.HasPrefix(text, "+"):
			added := strings.TrimPrefix(text, "+")
			trimmed := strings.TrimSpace(added)
			if !strings.HasSuffix(path, "_test.go") && !strings.HasPrefix(trimmed, "import ") && trimmed != "(" {
				sites = append(sites, sourceSite{path: path, line: line})
			}
			line++
			remaining--
		}
	}
	return sites
}

func mutationsAt(source, filename string, wanted int) []sourceMutation {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, filename, source, 0)
	if err != nil {
		return nil
	}
	fileToken := set.File(file.Pos())
	var mutations []sourceMutation
	seen := map[string]bool{}
	add := func(operator string, from, to token.Pos, replacement string) {
		start := fileToken.Offset(from)
		end := fileToken.Offset(to)
		if start < 0 || end < start || end > len(source) {
			return
		}
		changed := source[:start] + replacement + source[end:]
		key := operator + "\x00" + changed
		if !seen[key] {
			seen[key] = true
			mutations = append(mutations, sourceMutation{operator: operator, source: changed})
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if len(mutations) > 0 {
			return false
		}
		if node == nil || fileToken.Position(node.Pos()).Line != wanted {
			return true
		}
		switch n := node.(type) {
		case *ast.IfStmt:
			if n.Cond != nil && fileToken.Position(n.Cond.Pos()).Line == wanted {
				add("negate condition", n.Cond.Pos(), n.Cond.End(), "!("+source[fileToken.Offset(n.Cond.Pos()):fileToken.Offset(n.Cond.End())]+")")
			}
		case *ast.ForStmt:
			if n.Cond != nil && fileToken.Position(n.Cond.Pos()).Line == wanted {
				add("negate condition", n.Cond.Pos(), n.Cond.End(), "!("+source[fileToken.Offset(n.Cond.Pos()):fileToken.Offset(n.Cond.End())]+")")
			}
		case *ast.BinaryExpr:
			if comparison(n.Op) {
				add("flip comparison", n.OpPos, n.OpPos+token.Pos(len(n.Op.String())), flipped(n.Op).String())
			}
		case *ast.BasicLit:
			if replacement, ok := replacementLiteral(n); ok {
				add("replace literal", n.Pos(), n.End(), replacement)
			}
		case *ast.Ident:
			if n.Name == "true" || n.Name == "false" {
				add("replace literal", n.Pos(), n.End(), map[bool]string{true: "false", false: "true"}[n.Name == "true"])
			}
		}
		return true
	})
	if len(mutations) == 0 {
		ast.Inspect(file, func(node ast.Node) bool {
			statement, ok := node.(ast.Stmt)
			if !ok || !statementLine(fileToken, statement, wanted) || !droppable(statement) {
				return true
			}
			start := lineStart(source, fileToken.Offset(statement.Pos()))
			end := lineEnd(source, fileToken.Offset(statement.End()))
			add("drop statement", statement.Pos(), statement.End(), strings.Repeat(" ", end-start))
			return false
		})
	}
	return mutations
}

func comparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	default:
		return false
	}
}

func flipped(op token.Token) token.Token {
	switch op {
	case token.EQL:
		return token.NEQ
	case token.NEQ:
		return token.EQL
	case token.LSS:
		return token.GEQ
	case token.LEQ:
		return token.GTR
	case token.GTR:
		return token.LEQ
	default:
		return token.LSS
	}
}

func replacementLiteral(lit *ast.BasicLit) (string, bool) {
	switch lit.Kind {
	case token.STRING:
		if lit.Value == `"mutation"` {
			return `"mutated"`, true
		}
		return `"mutation"`, true
	case token.INT:
		if lit.Value == "0" {
			return "1", true
		}
		return "0", true
	case token.FLOAT:
		if lit.Value == "0.0" {
			return "1.0", true
		}
		return "0.0", true
	case token.CHAR:
		return `'x'`, true
	default:
		return "", false
	}
}

func statementLine(file *token.File, node ast.Node, wanted int) bool {
	return file.Position(node.Pos()).Line == wanted && file.Position(node.End()).Line == wanted
}

func droppable(node ast.Stmt) bool {
	switch node.(type) {
	case *ast.ExprStmt, *ast.AssignStmt, *ast.ReturnStmt, *ast.DeclStmt,
		*ast.IncDecStmt, *ast.SendStmt, *ast.GoStmt, *ast.DeferStmt,
		*ast.BranchStmt:
		return true
	default:
		return false
	}
}

func lineStart(source string, offset int) int {
	if offset > len(source) {
		offset = len(source)
	}
	if at := strings.LastIndex(source[:offset], "\n"); at >= 0 {
		return at + 1
	}
	return 0
}

func lineEnd(source string, offset int) int {
	if offset > len(source) {
		offset = len(source)
	}
	if at := strings.IndexByte(source[offset:], '\n'); at >= 0 {
		return offset + at
	}
	return len(source)
}

func copyCheckout(source string) (string, error) {
	destination, err := os.MkdirTemp("", "borg-mutant-checkout-")
	if err != nil {
		return "", fmt.Errorf("buildrunner: making mutant checkout: %w", err)
	}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) || rel == ".borg-module-cache" || strings.HasPrefix(rel, ".borg-module-cache"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		_ = os.RemoveAll(destination)
		return "", fmt.Errorf("buildrunner: copying mutant checkout: %w", err)
	}
	return destination, nil
}

func execCommand(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd
}
