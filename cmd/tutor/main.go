// Command tutor serves the API and PWA, or validates the content tree.
//
//	tutor serve              start the server (configured by environment)
//	tutor content validate   check every exercise, solution, and hint
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	contentfs "github.com/mayur-tolexo/tutor/content"
	"github.com/mayur-tolexo/tutor/internal/auth"
	"github.com/mayur-tolexo/tutor/internal/content"
	"github.com/mayur-tolexo/tutor/internal/grade"
	"github.com/mayur-tolexo/tutor/internal/httpapi"
	"github.com/mayur-tolexo/tutor/internal/runner"
	"github.com/mayur-tolexo/tutor/internal/store"
	"github.com/mayur-tolexo/tutor/internal/tutor"
	"github.com/mayur-tolexo/tutor/web"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"serve"}
	}
	var err error
	switch {
	case args[0] == "serve":
		err = serve(log)
	case args[0] == "content" && len(args) > 1 && args[1] == "validate":
		err = validate(log)
	default:
		err = fmt.Errorf("usage: tutor serve | tutor content validate")
	}
	if err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// contentFS returns the content tree: the CONTENT_DIR override for local
// authoring, else the copy embedded in the binary.
func contentFS() (fs.FS, error) {
	if dir := os.Getenv("CONTENT_DIR"); dir != "" {
		return os.DirFS(dir), nil
	}
	return contentfs.FS, nil
}

// validate loads the library, checks hint flags, and runs every reference
// solution against its own tests with the local runner. It exits non-zero on
// the first category of failure so CI output stays readable.
func validate(log *slog.Logger) error {
	root, err := contentFS()
	if err != nil {
		return err
	}
	lib, err := content.Load(root)
	if err != nil {
		return err
	}
	if errs := lib.CheckFlags(runner.KnownFlags); len(errs) > 0 {
		return errors.Join(errs...)
	}
	run := runner.NewLocal(4)
	ctx := context.Background()
	var failures []error
	for _, ex := range sortedExercises(lib) {
		res, err := run.Run(ctx, grade.SubmitSpec(ex, ex.Solution))
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: run solution: %w", ex.ID, err))
			continue
		}
		v := grade.Evaluate(ex, res)
		if !v.Passed {
			detail := v.Outcome
			for _, c := range v.Cases {
				if !c.Passed {
					detail += fmt.Sprintf("; case %s expected %q got %q", c.ID, c.Expected, c.Actual)
					if c.Error != nil {
						detail += fmt.Sprintf(" (%s: %s)", c.Error.Type, c.Error.Message)
					}
					break
				}
			}
			failures = append(failures, fmt.Errorf("%s: solution fails: %s", ex.ID, detail))
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	log.Info("content valid", "tracks", len(lib.Tracks), "exercises", len(lib.Exercises))
	return nil
}

// sortedExercises returns exercises in track order for stable output.
func sortedExercises(lib *content.Library) []*content.Exercise {
	var out []*content.Exercise
	seen := map[string]bool{}
	for _, t := range lib.Tracks {
		for _, u := range t.Units {
			for _, id := range u.Exercises {
				if ex := lib.Exercise(id); ex != nil && !seen[id] {
					out = append(out, ex)
					seen[id] = true
				}
			}
		}
	}
	// Exercises not yet placed in a track still get validated.
	for id, ex := range lib.Exercises {
		if !seen[id] {
			out = append(out, ex)
		}
	}
	return out
}

// serve builds every dependency from the environment and runs the HTTP server
// until SIGINT/SIGTERM.
func serve(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	root, err := contentFS()
	if err != nil {
		return err
	}
	lib, err := content.Load(root)
	if err != nil {
		return fmt.Errorf("content: %w", err)
	}
	if errs := lib.CheckFlags(runner.KnownFlags); len(errs) > 0 {
		return errors.Join(errs...)
	}

	var st store.Store
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pg, err := store.OpenPostgres(ctx, dsn)
		if err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
		defer pg.Close()
		st = pg
	} else {
		log.Warn("DATABASE_URL not set; using in-memory store (data is lost on restart)")
		st = store.NewMemory()
	}

	run, cleanup, err := buildRunner(ctx, log)
	if err != nil {
		return err
	}
	defer cleanup()

	svc := &tutor.Service{Store: st, DailyModelBudget: envInt("MODEL_DAILY_BUDGET", 0), Log: log}
	if base := os.Getenv("LLM_BASE_URL"); base != "" {
		svc.LLM = tutor.NewOpenAICompat(base, os.Getenv("LLM_API_KEY"), envOr("LLM_MODEL", "default"))
	} else {
		log.Warn("LLM_BASE_URL not set; hints are canned/generic only")
	}

	key := os.Getenv("SESSION_KEY")
	if key == "" {
		key = strings.Repeat("dev-only-insecure-key-", 2)
		log.Warn("SESSION_KEY not set; using an insecure development key")
	}
	sessions, err := auth.NewSessions([]byte(key), os.Getenv("INSECURE_COOKIES") == "")
	if err != nil {
		return err
	}

	srv := &httpapi.Server{
		Library: lib, Runner: run, Store: st, Tutor: svc, Sessions: sessions,
		Limits: httpapi.DefaultLimits, Static: web.Dist(), Log: log,
	}
	if cid := os.Getenv("GOOGLE_CLIENT_ID"); cid != "" {
		v, err := auth.NewGoogleVerifier(ctx, cid)
		if err != nil {
			return err
		}
		srv.Google, srv.GoogleClientID = v, cid
	}
	if srv.Static == nil {
		log.Warn("web/dist not built; serving API only")
	}

	addr := envOr("ADDR", ":8080")
	hs := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		hs.Shutdown(shutdownCtx)
	}()
	log.Info("listening", "addr", addr, "exercises", len(lib.Exercises))
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// buildRunner picks the sandbox backend: NeevCloud when NEEV_API_KEY is set,
// otherwise the unisolated local runner for development.
func buildRunner(ctx context.Context, log *slog.Logger) (runner.Runner, func(), error) {
	if os.Getenv("NEEV_API_KEY") == "" {
		log.Warn("NEEV_API_KEY not set; using the local runner (NO isolation, development only)")
		return runner.NewLocal(envInt("LOCAL_RUNNER_PARALLEL", 4)), func() {}, nil
	}
	nc, err := runner.NewNeevCloud(runner.NeevCloudConfig{
		APIBase:     envOr("NEEV_API_BASE", "https://api.ai.neevcloud.com/agent"),
		APIKey:      os.Getenv("NEEV_API_KEY"),
		OrgID:       os.Getenv("NEEV_ORG_ID"),
		ProjectID:   os.Getenv("NEEV_PROJECT_ID"),
		Region:      os.Getenv("NEEV_REGION"),
		TemplateID:  os.Getenv("NEEV_TEMPLATE_ID"),
		Python:      envOr("SANDBOX_PYTHON", "python3"),
		Prewarm:     envInt("SANDBOX_PREWARM", 2),
		MaxInflight: envInt("SANDBOX_MAX_INFLIGHT", 50),
		Logger:      log,
	})
	if err != nil {
		return nil, nil, err
	}
	nc.Start(ctx)
	return nc, nc.Stop, nil
}

// envOr returns the variable or a default.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt parses an integer variable, falling back to def when unset or invalid.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
