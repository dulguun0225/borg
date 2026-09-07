package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dulguun0225/borg/factory/clientdist"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/screens"
)

// serve is the process the lease was always for, per
// ../../../end-goal/one-process.md: one process that holds the lease for its own
// life, runs each component's pass on its own interval, and serves over HTTP
// between them. Every other subcommand acquires the lease, makes one pass, and
// exits, which is what ../../../roadmap.md#m8--the-screens-and-the-fleet says
// has to change before a screen can be reachable between two passes.
//
// What it serves is the four screens: package screens' own handler over the
// views and the calls this composition implements, with the client's build
// output embedded beside them, and GET /healthz beside that — the one route a
// reader outside the process compares the factory version on.

// defaultPort is where the screens will be served from, and where /healthz is
// served from until they are. It is a flag because an install may already be
// using it and nothing here can know.
const defaultPort = 8080

// serveCommand runs the process: the pool, the lease held for the life of it,
// the schema, the composition, the passes on their own intervals, and the HTTP
// server beside them. It returns when a signal ends it.
func serveCommand(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := flags.Int("port", defaultPort, "the port the factory serves on")
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	model := flags.String("model", "", "the provider's model id (required; the roadmap names the model in configuration)")
	provider := flags.String("provider", "openrouter", "which provider answers the model — "+providers+"; each reads its own credential from the secrets file")
	effort := flags.String("effort", "", "how long the model works before it answers, the field a fleet entry has for it; empty asks for none")
	var services serviceFlag
	flags.Var(&services, "service", "a service as name=path, the path being its git repository (created when absent); given once per service, and at least once")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the owner every authoring write this process makes is made as")
	projectName := flags.String("project", defaultProjectName, "the project this process installs and works in, created where it does not exist")
	areaName := flags.String("area", "", "the area items are in, declared where it does not exist")
	pace := flags.Duration("pace", 2*time.Second, "the least time between two model calls; 0 sends them back to back")
	ceiling := flags.Int("candidate-environments", 8, "how many candidate environments this platform has room for at once")
	watchEvery := flags.Duration("watch-every", time.Second, "how often the watch reads the quantity")
	every := intervalFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	for _, required := range []struct{ name, value string }{
		{"secrets", *secrets}, {"model", *model}, {"targets", *targets},
	} {
		if required.value == "" {
			return fmt.Errorf("factory serve: -%s is required", required.name)
		}
	}
	if len(services) == 0 {
		return errors.New("factory serve: -service is required, at least once")
	}

	resolver, err := secretsResolver(*secrets)
	if err != nil {
		return err
	}
	// The provider is resolved here for the flag's sake alone: a -provider this
	// interface does not implement is refused at the flag rather than at the
	// first dispatch.
	if _, err := newModel(*provider, *model, resolver); err != nil {
		return err
	}

	// The signal is what ends this process, and it ends the passes, the server
	// and the lease in that order.
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	pool, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	token, stopLease, err := acquireLease(ctx, pool)
	if err != nil {
		return err
	}
	defer stopLease()
	if _, err := postgres.Start(ctx, pool); err != nil {
		return err
	}
	driftStore, shut, err := openDriftDetector(ctx)
	if err != nil {
		return err
	}
	defer shut()

	p, err := compose(ctx, deps{
		pool:                pool,
		token:               token,
		modelName:           *model,
		modelCredentialName: modelCredentialNameFor(*provider),
		effort:              *effort,
		modelFor:            modelsPerEntry(resolver, *pace),
		targets:             newTargetSet(localTargetAt),
		dir:                 *targets,
		project:             *projectName,
		// The process installs the way run does: it is the process an install
		// starts, so the project and production's environment for it are
		// created here where they do not exist.
		install:          true,
		credential:       deployCredential(),
		out:              os.Stdout,
		human:            *human,
		services:         services,
		area:             *areaName,
		candidateCeiling: *ceiling,
		driftdetector:    driftStore,
		// The watch is a pass of its own on its own interval, so the advance
		// pass takes one reading and leaves what is still open to it rather than
		// waiting a deadline out inside a tick.
		watchFor:   0,
		watchEvery: *watchEvery,
	})
	if err != nil {
		return err
	}

	// The screens, composed over this same path: the views map every record
	// onto the view package screens serves, and the calls reach the writer a
	// subcommand reaches. The server is what tells a subscriber a record
	// changed, so the calls are handed it once it exists.
	views := &views{p: p}
	made := &calls{p: p, v: views}
	screenServer := screens.New(views, made, factoryVersion, clientdist.Browser())
	made.server = screenServer

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		Handler:           served(screenServer),
		ReadHeaderTimeout: 10 * time.Second,
	}
	served := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stdout, "The factory serves the four screens on :%d; GET /healthz answers with the factory version\n", *port)
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		served <- err
	}()

	passes := newPasses(p, every, screenServer.Changed)
	done := make(chan struct{})
	go func() {
		passes.Run(ctx)
		close(done)
	}()

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stdout, "A signal ended the process; the passes stop, the server shuts down, and the lease is released")
	case err := <-served:
		if err != nil {
			// The port is the one thing this process cannot do without: a
			// factory that runs its passes and serves nothing is a factory with
			// no screens, which is what this process exists to be.
			stopSignals()
			<-done
			shutDown(server)
			return err
		}
	}
	<-done
	shutDown(server)
	return nil
}

// served is what this process answers on its port: the four screens, and
// GET /healthz beside them.
//
// /healthz is not one of the screens' own routes and carries neither the
// factory version nor a principal, because it is what a reader outside the
// process reads before it has either: an upgrade is applied by whoever hosts
// the install, and this is how they see which version is running.
func served(screenServer *screens.Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, factoryVersion)
	})
	mux.Handle("/", screenServer)
	return mux
}

// shutDown ends the server, giving what is in flight a bounded moment to
// finish. The context is not the process's: that one is already cancelled by the
// signal, and a shutdown on it would close every connection at once.
func shutDown(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
