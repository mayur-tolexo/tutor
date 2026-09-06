package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePlatform emulates the control plane (create/get/delete) and a sandboxd
// data plane (files/write, exec) closely enough to exercise the runner's flow.
type fakePlatform struct {
	mu           sync.Mutex
	created      atomic.Int32
	deleted      atomic.Int32
	files        map[string]map[string]string // sandbox id -> path -> content
	readyAfter   int                          // GETs before Ready
	gets         map[string]int
	execOut      string
	execExit     int
	createStatus int
	cp, dp       *httptest.Server
}

func newFakePlatform(t *testing.T) *fakePlatform {
	f := &fakePlatform{files: map[string]map[string]string{}, gets: map[string]int{}, execOut: `{"syntax_error":null,"flags":[],"cases":[]}`, createStatus: http.StatusCreated}
	f.dp = httptest.NewServer(http.HandlerFunc(f.dataPlane))
	f.cp = httptest.NewServer(http.HandlerFunc(f.controlPlane))
	t.Cleanup(func() { f.cp.Close(); f.dp.Close() })
	return f
}

func (f *fakePlatform) controlPlane(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer key" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	const prefix = "/agent/api/v1beta1/orgs/org/projects/proj/sandboxes"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, prefix), "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && id == "":
		if f.createStatus != http.StatusCreated {
			w.WriteHeader(f.createStatus)
			return
		}
		n := f.created.Add(1)
		id = fmt.Sprintf("sb-%d", n)
		f.files[id] = map[string]string{}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": id, "phase": "Pending"})
	case r.Method == http.MethodGet:
		f.gets[id]++
		phase, connect := "Pending", ""
		if f.gets[id] >= f.readyAfter {
			phase, connect = "Ready", f.dp.URL+"/"+id
		}
		json.NewEncoder(w).Encode(map[string]any{"id": id, "phase": phase, "connect_url": connect})
	case r.Method == http.MethodDelete:
		f.deleted.Add(1)
		delete(f.files, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakePlatform) dataPlane(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Api-Key") != "key" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	id, rest := parts[0], parts[1]
	f.mu.Lock()
	defer f.mu.Unlock()
	switch rest {
	case "v1/files/write":
		body, _ := io.ReadAll(r.Body)
		f.files[id][r.URL.Query().Get("path")] = string(body)
		json.NewEncoder(w).Encode(map[string]int{"bytes_written": len(body)})
	case "v1/exec":
		var req execRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Command != "python3" || len(req.Args) != 2 || req.Args[0] != HarnessFile {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := base64.StdEncoding.EncodeToString([]byte(f.execOut))
		fmt.Fprintf(w, `{"type":"stdout","data":"%s"}`+"\n", enc)
		fmt.Fprintf(w, `{"type":"stderr","data":"%s"}`+"\n", base64.StdEncoding.EncodeToString([]byte("warn")))
		fmt.Fprintf(w, `{"type":"exit","exit_code":%d}`+"\n", f.execExit)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTestRunner(t *testing.T, f *fakePlatform, prewarm int) *NeevCloud {
	n, err := NewNeevCloud(NeevCloudConfig{
		APIBase: f.cp.URL + "/agent", APIKey: "key", OrgID: "org", ProjectID: "proj",
		Prewarm: prewarm, ReadyTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Drain async deletes before the fake servers close.
	t.Cleanup(n.Stop)
	return n
}

func TestNeevCloudRunStagesFilesExecsAndDeletes(t *testing.T) {
	f := newFakePlatform(t)
	f.readyAfter = 2
	f.execOut = `{"syntax_error":null,"flags":["no_input_call"],"cases":[{"id":"a","stdout":"hi\n","stderr":"","exit_code":0,"timed_out":false,"files":{},"error":null}]}`
	n := newTestRunner(t, f, 0)

	res, err := n.Run(context.Background(), RunSpec{Code: "print('hi')", Cases: []Case{{ID: "a"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Cases) != 1 || res.Cases[0].Stdout != "hi\n" || res.Flags[0] != "no_input_call" {
		t.Errorf("result = %+v", res)
	}
	n.wg.Wait() // async delete
	if f.created.Load() != 1 || f.deleted.Load() != 1 {
		t.Errorf("created=%d deleted=%d", f.created.Load(), f.deleted.Load())
	}
}

func TestNeevCloudWritesAllThreeFiles(t *testing.T) {
	f := newFakePlatform(t)
	f.readyAfter = 1
	var seen map[string]string
	// Capture the staged files before the async delete removes them.
	f.dp.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.dataPlane(w, r)
		if strings.HasSuffix(r.URL.Path, "v1/exec") {
			f.mu.Lock()
			seen = map[string]string{}
			for k, v := range f.files["sb-1"] {
				seen[k] = v
			}
			f.mu.Unlock()
		}
	})
	n := newTestRunner(t, f, 0)
	if _, err := n.Run(context.Background(), RunSpec{Code: "x=1", Cases: []Case{{ID: "a", Stdin: "in"}}}); err != nil {
		t.Fatal(err)
	}
	if seen[MainFile] != "x=1" || seen[HarnessFile] != Harness || !strings.Contains(seen[SpecFile], `"stdin":"in"`) {
		t.Errorf("staged files = %v", keys(seen))
	}
}

func TestNeevCloudCreateBusyMapsToErrBusy(t *testing.T) {
	f := newFakePlatform(t)
	f.createStatus = http.StatusTooManyRequests
	n := newTestRunner(t, f, 0)
	if _, err := n.Run(context.Background(), RunSpec{Code: "x"}); err != ErrBusy {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}

func TestNeevCloudHarnessExitNonZeroIsError(t *testing.T) {
	f := newFakePlatform(t)
	f.readyAfter = 1
	f.execExit = 1
	n := newTestRunner(t, f, 0)
	_, err := n.Run(context.Background(), RunSpec{Code: "x"})
	if err == nil || !strings.Contains(err.Error(), "harness exited 1") {
		t.Errorf("err = %v", err)
	}
}

func TestNeevCloudPrewarmPoolIsUsedAndRefilled(t *testing.T) {
	f := newFakePlatform(t)
	f.readyAfter = 1
	n := newTestRunner(t, f, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)
	waitFor(t, func() bool { return len(n.pool) == 1 })

	if _, err := n.Run(ctx, RunSpec{Code: "x"}); err != nil {
		t.Fatal(err)
	}
	// The run consumed the pooled sandbox; the loop must create a replacement.
	waitFor(t, func() bool { return f.created.Load() >= 2 && len(n.pool) == 1 })
	cancel()
	n.Stop()
	if f.deleted.Load() != f.created.Load() {
		t.Errorf("leaked sandboxes: created=%d deleted=%d", f.created.Load(), f.deleted.Load())
	}
}

func TestNeevCloudMaxInflight(t *testing.T) {
	f := newFakePlatform(t)
	n := newTestRunner(t, f, 0)
	n.slots = make(chan struct{}, 1)
	n.slots <- struct{}{} // occupy the only slot
	if _, err := n.Run(context.Background(), RunSpec{Code: "x"}); err != ErrBusy {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}

// waitFor polls cond for up to two seconds.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

// keys lists a map's keys for error messages.
func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
