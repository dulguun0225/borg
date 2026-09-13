package buildrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dulguun0225/borg/factory/build"
)

type GitClone struct{}

func (GitClone) Select(ctx context.Context, req SelectionRequest) (Checkout, error) {
	if req.Directory == "" {
		return Checkout{}, errors.New("buildrunner: selection has no directory")
	}
	if req.Credential == "" {
		return Checkout{}, errors.New("buildrunner: repository selection needs a credential")
	}
	repository := req.Repository
	if repository == "" {
		repository = req.Directory
	}
	if _, err := os.Stat(filepath.Join(req.Directory, ".git")); os.IsNotExist(err) {
		if repository == req.Directory {
			if err := os.MkdirAll(req.Directory, 0o755); err != nil {
				return Checkout{}, fmt.Errorf("buildrunner: creating the repository directory: %w", err)
			}
			if _, err := gitCredential(ctx, req.Directory, req.Credential, "init"); err != nil {
				return Checkout{}, err
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(req.Directory), 0o755); err != nil {
				return Checkout{}, fmt.Errorf("buildrunner: creating the clone parent: %w", err)
			}
			if _, err := gitCredential(ctx, filepath.Dir(req.Directory), req.Credential, "clone", repository, req.Directory); err != nil {
				return Checkout{}, err
			}
		}
	} else if err != nil {
		return Checkout{}, fmt.Errorf("buildrunner: inspecting the repository directory: %w", err)
	} else if remotes, err := gitCredential(ctx, req.Directory, req.Credential, "remote"); err != nil {
		return Checkout{}, err
	} else if strings.TrimSpace(remotes) != "" {
		if _, err := gitCredential(ctx, req.Directory, req.Credential, "fetch", "--all", "--prune"); err != nil {
			return Checkout{}, err
		}
	}
	head, headErr := gitCredential(ctx, req.Directory, req.Credential, "rev-parse", "HEAD")
	branchBase := req.Base
	if branchBase == "" && req.Repository != "" && headErr == nil {
		branchBase = head
	}
	switch req.Mode {
	case DetachedCommit:
		if _, err := gitCredential(ctx, req.Directory, req.Credential, "switch", "--detach", req.Commit); err != nil {
			return Checkout{}, err
		}
	case CandidateBranch:
		if req.Branch == "" {
			return Checkout{}, errors.New("buildrunner: candidate selection has no branch")
		}
		if _, err := gitCredential(ctx, req.Directory, req.Credential, "switch", req.Branch); err != nil {
			if branchBase != "" {
				if _, err = gitCredential(ctx, req.Directory, req.Credential, "switch", "-c", req.Branch, branchBase); err != nil {
					return Checkout{}, err
				}
			} else if _, err = gitCredential(ctx, req.Directory, req.Credential, "switch", "--orphan", req.Branch); err != nil {
				return Checkout{}, err
			}
		}
	default:
		return Checkout{}, errors.New("buildrunner: unknown selection mode")
	}
	commit, err := gitCredential(ctx, req.Directory, req.Credential, "rev-parse", "HEAD")
	if err != nil {
		if req.Mode == CandidateBranch && req.Commit == "" {
			return Checkout{Directory: req.Directory, Branch: req.Branch, Base: req.Base}, nil
		}
		return Checkout{}, err
	}
	return Checkout{Directory: req.Directory, Branch: req.Branch, Base: req.Base, Commit: commit}, nil
}

func (GitClone) Push(ctx context.Context, req PushRequest) error {
	if req.Branch == "" || req.Branch == "master" || req.Branch != req.Checkout.Branch {
		return errors.New("buildrunner: pushes are permitted only to the selected candidate branch")
	}
	if req.Credential == "" {
		return errors.New("buildrunner: a candidate push needs a repository credential")
	}
	_, err := gitCredential(ctx, req.Checkout.Directory, req.Credential, "push", "origin", req.Branch+":"+req.Branch)
	return err
}

type GoResolver struct{}

func moduleLicence(cache, modulePath, version string) string {
	moduleDir := filepath.Join(cache, escapeModulePath(modulePath)+"@"+version)
	for _, name := range []string{"LICENSE", "LICENCE", "COPYING"} {
		if _, err := os.Stat(filepath.Join(moduleDir, name)); err == nil {
			return name
		}
	}
	return "could not derive"
}

func escapeModulePath(path string) string {
	var escaped strings.Builder
	for _, r := range path {
		if r >= 'A' && r <= 'Z' {
			escaped.WriteByte('!')
			escaped.WriteByte(byte(r + ('a' - 'A')))
			continue
		}
		escaped.WriteRune(r)
	}
	return escaped.String()
}

type goDependencyLists struct {
	runtime []string
	test    []string
}

var goDependencyCache sync.Map

func (GoResolver) Resolve(ctx context.Context, checkout Checkout, repositoryCredential string, registryCredentials []string) (Resolution, error) {
	module, err := os.ReadFile(filepath.Join(checkout.Directory, "go.mod"))
	if err != nil {
		return Resolution{CouldNotDerive: err.Error()}, nil
	}
	modulePath := ""
	for _, line := range strings.Split(string(module), "\n") {
		if after, ok := strings.CutPrefix(line, "module "); ok {
			modulePath = strings.TrimSpace(after)
		}
	}
	if modulePath == "" {
		return Resolution{CouldNotDerive: "go.mod names no module"}, nil
	}
	cache := filepath.Join(checkout.Directory, ".borg-module-cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return Resolution{CouldNotDerive: err.Error()}, nil
	}
	fetch := exec.CommandContext(ctx, "go", "mod", "download")
	fetch.Dir = checkout.Directory
	fetch.Env = resolverEnvironment(checkout.Directory, cache, repositoryCredential, registryCredentials)
	// Go's module downloader has no install-time script hook, so there is no install script to enable.
	if out, err := fetch.CombinedOutput(); err != nil {
		return Resolution{CouldNotDerive: fmt.Sprintf("go mod download: %v: %s", err, strings.TrimSpace(string(out)))}, nil
	}
	content, err := os.ReadFile(filepath.Join(checkout.Directory, "go.sum"))
	if os.IsNotExist(err) {
		return Resolution{Coverage: []build.Coverage{goCoverage(false)}, ModuleCache: cache}, nil
	}
	if err != nil {
		return Resolution{CouldNotDerive: err.Error()}, nil
	}
	env := resolverEnvironment(checkout.Directory, cache, repositoryCredential, registryCredentials)
	var runtimePackages, testPackages []string
	cacheKey := checkout.Directory + "\x00" + checkout.Commit + "\x00" + goProxy()
	if checkout.Commit != "" {
		if cached, ok := goDependencyCache.Load(cacheKey); ok {
			lists := cached.(goDependencyLists)
			runtimePackages, testPackages = lists.runtime, lists.test
		}
	}
	if runtimePackages == nil {
		runtimePackages, err = goList(ctx, checkout.Directory, env, false)
		if err != nil {
			return Resolution{CouldNotDerive: err.Error()}, nil
		}
		testPackages = runtimePackages
		if hasGoTests(checkout.Directory) {
			testPackages, err = goList(ctx, checkout.Directory, env, true)
			if err != nil {
				return Resolution{CouldNotDerive: err.Error()}, nil
			}
		}
		if checkout.Commit != "" {
			goDependencyCache.Store(cacheKey, goDependencyLists{runtime: runtimePackages, test: testPackages})
		}
	}
	var entries []build.ResolvedEntry
	seen := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.HasSuffix(fields[1], "/go.mod") {
			continue
		}
		key := fields[0] + "@" + fields[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		runTime := packageInList(runtimePackages, fields[0])
		buildTime := packageInList(testPackages, fields[0])
		if !runTime && !buildTime {
			buildTime = true
		}
		entries = append(entries, build.ResolvedEntry{Ecosystem: "go", Source: goModuleSource(fields[0]),
			Package: fields[0], Version: fields[1], Digest: fields[2], Licence: moduleLicence(cache, fields[0], fields[1]), RequiredBy: modulePath,
			RunTime: runTime, BuildTime: buildTime})
	}
	return Resolution{Entries: entries, Coverage: []build.Coverage{goCoverage(enumeratedVendor(runtimePackages, testPackages))}, ModuleCache: cache}, nil
}

type GoSchema struct{}

func (GoSchema) Read(ctx context.Context, checkout Checkout) (SchemaReading, error) {
	args := []string{"diff", "--name-only", checkout.BaseOrEmpty(), checkout.Commit}
	if checkout.Base == "" {
		args = []string{"diff", "--root", "--name-only", checkout.Commit}
	}
	out, err := git(ctx, checkout.Directory, args...)
	if err != nil {
		return SchemaReading{}, nil
	}
	reading := SchemaReading{}
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		for _, part := range strings.Split(name, "/") {
			if part == "migrations" || part == "schema" {
				reading.Declares = true
				content, readErr := os.ReadFile(filepath.Join(checkout.Directory, name))
				if readErr == nil {
					for n, line := range strings.Split(string(content), "\n") {
						if strings.Contains(strings.ToLower(line), "deprecated") {
							reading.Marks = append(reading.Marks, fmt.Sprintf("%s:%d: %s", name, n+1, strings.TrimSpace(line)))
						}
					}
				}
				break
			}
		}
	}
	return reading, nil
}

func (c Checkout) BaseOrEmpty() string {
	if c.Base != "" {
		return c.Base
	}
	return EmptyTree
}

type GoProcess struct{}

func (GoProcess) Run(ctx context.Context, in ProcessInput) (ProcessOutput, error) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return ProcessOutput{}, err
	}
	cmd := exec.CommandContext(ctx, goPath, "build", "-overlay", in.Overlay, "-o", in.Output, ".")
	cmd.Dir = in.Checkout.Directory
	cache := in.Resolution.ModuleCache
	if cache == "" {
		cache = filepath.Join(in.Checkout.Directory, ".borg-module-cache")
	}
	cmd.Env = []string{"PATH=" + filepath.Dir(goPath), "GOPROXY=off", "GOFLAGS=-mod=mod",
		"GOSUMDB=off", "GOMODCACHE=" + cache, "GOCACHE=" + filepath.Join(filepath.Dir(in.Output), ".go-cache"),
		"HOME=" + filepath.Join(filepath.Dir(in.Output), ".home")}
	if out, err := cmd.CombinedOutput(); err != nil {
		return ProcessOutput{}, fmt.Errorf("%w: %s", ErrDoesNotCompile, strings.TrimSpace(string(out)))
	}
	content, err := os.ReadFile(in.Output)
	if err != nil {
		return ProcessOutput{}, fmt.Errorf("buildrunner: reading the compiled artifact: %w", err)
	}
	digest := sha256.Sum256(content)
	return ProcessOutput{ArtifactDigest: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

func git(ctx context.Context, directory string, args ...string) (string, error) {
	return gitWithEnv(ctx, directory, nil, args...)
}

func gitCredential(ctx context.Context, directory, credential string, args ...string) (string, error) {
	if credential == "" {
		return git(ctx, directory, args...)
	}
	env := []string{"BORG_GIT_CREDENTIAL=" + credential, "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=!f() { printf 'username=git\\npassword=%s\\n' \"$BORG_GIT_CREDENTIAL\"; }; f"}
	return gitWithEnv(ctx, directory, env, args...)
}

func gitWithEnv(ctx context.Context, directory string, extra []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = directory
	if extra != nil {
		cmd.Env = append(os.Environ(), extra...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("buildrunner: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func resolverEnvironment(_ string, cache, repositoryCredential string, registryCredentials []string) []string {
	home := filepath.Join(os.TempDir(), "borg-fetch-home")
	_ = os.MkdirAll(home, 0o755)
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GOMODCACHE=" + cache,
		"GOFLAGS=-mod=mod", "GOSUMDB=off", "GOTELEMETRY=off", "GOPROXY=" + goProxy(),
		"BORG_REPOSITORY_CREDENTIAL=" + repositoryCredential,
		"BORG_REGISTRY_CREDENTIALS=" + strings.Join(registryCredentials, "\x00")}
}

func goProxy() string {
	if value := os.Getenv("GOPROXY"); value != "" {
		return value
	}
	return "https://proxy.golang.org,direct"
}

func goModuleSource(modulePath string) string {
	proxy := goProxy()
	if proxy == "direct" {
		return "direct:" + strings.Split(modulePath, "/")[0]
	}
	return proxy
}

func goList(ctx context.Context, directory string, environment []string, tests bool) ([]string, error) {
	args := []string{"list", "-deps"}
	if tests {
		args = append(args, "-test")
	}
	args = append(args, ".")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = directory
	cmd.Env = environment
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go list dependencies: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.Fields(string(out)), nil
}

func packageInList(packages []string, module string) bool {
	for _, packagePath := range packages {
		if packagePath == module || strings.HasPrefix(packagePath, module+"/") {
			return true
		}
	}
	return false
}

func hasGoTests(directory string) bool {
	found := false
	_ = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".borg-module-cache", "vendor":
				if path != directory {
					return fs.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			found = true
		}
		return nil
	})
	return found
}

func goCoverage(enumeratedVendor bool) build.Coverage {
	return build.Coverage{Ecosystem: "go", Source: goProxy(), BaseImagePackages: false,
		VendoredSource: enumeratedVendor, StaticallyLinkedCode: true, Digests: true, FetchWithoutRunning: true}
}

func enumeratedVendor(runtime, tests []string) bool {
	for _, packagePath := range append(append([]string{}, runtime...), tests...) {
		if strings.Contains(packagePath, "/vendor/") || strings.HasPrefix(packagePath, "vendor/") {
			return true
		}
	}
	return false
}
