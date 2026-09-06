package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// NeevCloudConfig configures the sandbox-backed runner.
type NeevCloudConfig struct {
	APIBase    string // control plane, e.g. https://api.ai.neevcloud.com/agent
	APIKey     string
	OrgID      string
	ProjectID  string
	Region     string // optional; platform default when empty
	TemplateID string // optional; leaving it empty keeps creates warm-pool eligible
	Python     string // interpreter inside the sandbox; defaults to python3
	// Prewarm is how many Ready sandboxes to keep on hand so a run skips
	// create+ready latency. 0 disables prewarming.
	Prewarm int
	// MaxInflight caps sandboxes in use at once; beyond it Run returns ErrBusy.
	MaxInflight  int
	ReadyTimeout time.Duration
	Logger       *slog.Logger
}

// NeevCloud runs each attempt in a fresh sandbox: create (or take a prewarmed
// one), write files, exec the harness, delete. Sandboxes are never shared
// between attempts.
type NeevCloud struct {
	cfg    NeevCloudConfig
	http   *http.Client
	pool   chan *sandbox
	slots  chan struct{}
	log    *slog.Logger
	wg     sync.WaitGroup
	stopCh chan struct{}
	stop   sync.Once
}

// sandbox is the subset of the platform's sandbox record the runner needs.
type sandbox struct {
	ID         string `json:"id"`
	Phase      string `json:"phase"`
	ConnectURL string `json:"connect_url"`
}

// NewNeevCloud validates cfg and returns a runner. Call Start to begin
// prewarming and Stop to release pooled sandboxes.
func NewNeevCloud(cfg NeevCloudConfig) (*NeevCloud, error) {
	if cfg.APIBase == "" || cfg.APIKey == "" || cfg.OrgID == "" || cfg.ProjectID == "" {
		return nil, errors.New("neevcloud runner: api base, api key, org id and project id are required")
	}
	if cfg.Python == "" {
		cfg.Python = "python3"
	}
	if cfg.MaxInflight < 1 {
		cfg.MaxInflight = 50
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 90 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &NeevCloud{
		cfg: cfg,
		// No client-wide timeout: exec streams for the harness's full budget;
		// every call carries its own context deadline instead.
		http:   &http.Client{},
		pool:   make(chan *sandbox, max(cfg.Prewarm, 1)),
		slots:  make(chan struct{}, cfg.MaxInflight),
		log:    cfg.Logger,
		stopCh: make(chan struct{}),
	}, nil
}

// Start launches the prewarm loop, which tops the pool up whenever a sandbox
// is taken. It returns immediately.
func (n *NeevCloud) Start(ctx context.Context) {
	if n.cfg.Prewarm <= 0 {
		return
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-n.stopCh:
				return
			default:
			}
			if len(n.pool) >= n.cfg.Prewarm {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			sb, err := n.create(ctx)
			if err != nil {
				// Back off so a platform outage does not turn into a create storm.
				n.log.Warn("prewarm create failed", "err", err)
				time.Sleep(5 * time.Second)
				continue
			}
			select {
			case n.pool <- sb:
			default:
				n.deleteAsync(sb)
			}
		}
	}()
}

// Stop ends prewarming, waits for in-flight deletes, and deletes every pooled
// sandbox. It is safe to call more than once.
func (n *NeevCloud) Stop() {
	n.stop.Do(func() {
		close(n.stopCh)
		n.wg.Wait()
		for {
			select {
			case sb := <-n.pool:
				n.delete(context.Background(), sb)
			default:
				return
			}
		}
	})
}

// Run executes spec in a fresh sandbox and always deletes it afterwards.
func (n *NeevCloud) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	select {
	case n.slots <- struct{}{}:
		defer func() { <-n.slots }()
	default:
		return RunResult{}, ErrBusy
	}

	sb, err := n.acquire(ctx)
	if err != nil {
		return RunResult{}, err
	}
	defer n.deleteAsync(sb)

	specBytes, err := spec.specJSON()
	if err != nil {
		return RunResult{}, err
	}
	for name, data := range map[string]string{HarnessFile: Harness, MainFile: spec.Code, SpecFile: string(specBytes)} {
		if err := n.writeFile(ctx, sb, name, data); err != nil {
			return RunResult{}, fmt.Errorf("write %s: %w", name, err)
		}
	}

	budget := spec.TotalBudget()
	execCtx, cancel := context.WithTimeout(ctx, budget+10*time.Second)
	defer cancel()
	start := time.Now()
	stdout, stderr, err := n.exec(execCtx, sb, budget)
	if err != nil {
		return RunResult{}, err
	}
	r, err := parseResult(stdout, stderr)
	r.Duration = time.Since(start)
	return r, err
}

// acquire returns a Ready sandbox, preferring the prewarmed pool. A pooled
// sandbox is re-checked because the platform may have paused it while idle.
func (n *NeevCloud) acquire(ctx context.Context) (*sandbox, error) {
	for {
		select {
		case sb := <-n.pool:
			cur, err := n.get(ctx, sb.ID)
			if err == nil && cur.Phase == "Ready" && cur.ConnectURL != "" {
				return cur, nil
			}
			n.deleteAsync(sb)
			continue
		default:
			return n.create(ctx)
		}
	}
}

// createRequest is the control-plane create body. Env and disk are left unset
// on purpose so the platform can serve the create from its warm pool.
type createRequest struct {
	Region            string     `json:"region,omitempty"`
	SandboxTemplateID string     `json:"sandbox_template_id,omitempty"`
	Egress            *egress    `json:"egress,omitempty"`
	Lifecycle         *lifecycle `json:"lifecycle,omitempty"`
}

type egress struct {
	Mode string `json:"mode"`
}

type lifecycle struct {
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds"`
	MaxLifetimeSeconds int    `json:"max_lifetime_seconds"`
	OnIdle             string `json:"on_idle"`
}

// create makes a sandbox and waits until it is Ready with a connect URL.
// Student code needs no network, so egress is denied outright; a short max
// lifetime ensures a sandbox leaked by a crash is reclaimed by the platform.
func (n *NeevCloud) create(ctx context.Context) (*sandbox, error) {
	body := createRequest{
		Region:            n.cfg.Region,
		SandboxTemplateID: n.cfg.TemplateID,
		Egress:            &egress{Mode: "deny_all"},
		Lifecycle:         &lifecycle{IdleTimeoutSeconds: 900, MaxLifetimeSeconds: 1800, OnIdle: "delete"},
	}
	var sb sandbox
	status, err := n.controlJSON(ctx, http.MethodPost, n.sandboxesPath(""), body, &sb)
	if err != nil {
		return nil, err
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return nil, ErrBusy
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return nil, fmt.Errorf("create sandbox: status %d", status)
	}

	deadline := time.Now().Add(n.cfg.ReadyTimeout)
	for sb.Phase != "Ready" || sb.ConnectURL == "" {
		if sb.Phase == "Paused" || sb.Phase == "RestoreFailed" || time.Now().After(deadline) {
			n.deleteAsync(&sb)
			return nil, fmt.Errorf("sandbox %s not ready: phase %q", sb.ID, sb.Phase)
		}
		select {
		case <-ctx.Done():
			n.deleteAsync(&sb)
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		cur, err := n.get(ctx, sb.ID)
		if err != nil {
			n.deleteAsync(&sb)
			return nil, err
		}
		sb = *cur
	}
	return &sb, nil
}

// get fetches the current sandbox record.
func (n *NeevCloud) get(ctx context.Context, id string) (*sandbox, error) {
	var sb sandbox
	status, err := n.controlJSON(ctx, http.MethodGet, n.sandboxesPath(id), nil, &sb)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("get sandbox %s: status %d", id, status)
	}
	return &sb, nil
}

// delete removes the sandbox; a 404 means it is already gone.
func (n *NeevCloud) delete(ctx context.Context, sb *sandbox) {
	if sb == nil || sb.ID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, err := n.controlJSON(ctx, http.MethodDelete, n.sandboxesPath(sb.ID), nil, nil)
	if err != nil || (status != http.StatusNoContent && status != http.StatusOK && status != http.StatusNotFound) {
		n.log.Warn("delete sandbox failed", "id", sb.ID, "status", status, "err", err)
	}
}

// deleteAsync deletes off the request path so a slow control plane never
// delays the student's result.
func (n *NeevCloud) deleteAsync(sb *sandbox) {
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		n.delete(context.Background(), sb)
	}()
}

// sandboxesPath builds the org/project-scoped sandbox collection or item path.
func (n *NeevCloud) sandboxesPath(id string) string {
	p := fmt.Sprintf("%s/api/v1beta1/orgs/%s/projects/%s/sandboxes", strings.TrimRight(n.cfg.APIBase, "/"), n.cfg.OrgID, n.cfg.ProjectID)
	if id != "" {
		p += "/" + id
	}
	return p
}

// controlJSON performs a control-plane call with bearer auth, decoding a JSON
// response into out when non-nil. It returns the status for the caller to
// interpret rather than treating non-2xx as an error.
func (n *NeevCloud) controlJSON(ctx context.Context, method, url string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+n.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s %s: %w", method, url, err)
		}
	} else {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	}
	return resp.StatusCode, nil
}

// writeFile puts one workspace-relative file into the sandbox via sandboxd.
func (n *NeevCloud) writeFile(ctx context.Context, sb *sandbox, name, data string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	u := strings.TrimRight(sb.ConnectURL, "/") + "/v1/files/write?path=" + url.QueryEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(data))
	if err != nil {
		return err
	}
	n.dataHeaders(req)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// execRequest and execEvent are sandboxd's exec request and NDJSON frames.
type execRequest struct {
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	TimeoutMS int      `json:"timeout_ms,omitempty"`
}

type execEvent struct {
	Type       string `json:"type"`
	Data       []byte `json:"data,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

// exec runs the harness and assembles its streamed stdout and stderr. A
// non-zero exit is reported as an error because the harness itself never exits
// non-zero on student failures.
func (n *NeevCloud) exec(ctx context.Context, sb *sandbox, budget time.Duration) ([]byte, string, error) {
	body, _ := json.Marshal(execRequest{
		Command:   n.cfg.Python,
		Args:      []string{HarnessFile, SpecFile},
		TimeoutMS: int(budget / time.Millisecond),
	})
	u := strings.TrimRight(sb.ConnectURL, "/") + "/v1/exec"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	n.dataHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	resp, err := n.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, "", fmt.Errorf("exec: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var stdout, stderr bytes.Buffer
	exit := -1
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev execEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, "", fmt.Errorf("exec frame: %w", err)
		}
		switch ev.Type {
		case "stdout":
			stdout.Write(ev.Data)
		case "stderr":
			stderr.Write(ev.Data)
		case "exit":
			if ev.ExitCode != nil {
				exit = *ev.ExitCode
			}
		case "error":
			return nil, "", fmt.Errorf("exec: %s: %s", ev.ReasonCode, ev.Message)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, "", fmt.Errorf("exec stream: %w", err)
	}
	if exit != 0 {
		return nil, "", fmt.Errorf("harness exited %d: %s", exit, trunc(stderr.String(), 500))
	}
	return stdout.Bytes(), stderr.String(), nil
}

// dataHeaders sets sandboxd auth on a data-plane request.
func (n *NeevCloud) dataHeaders(req *http.Request) {
	req.Header.Set("X-Api-Key", n.cfg.APIKey)
	req.Header.Set("X-Protocol-Version", "1")
}
