# tutor

Free, phone-first Python practice for Indian Class 11 students, built around
the CBSE Computer Science practical. A student opens a link, writes Python on
their phone against a syllabus-ordered set of exercises, runs it in an
isolated sandbox, and gets a Hinglish tutor that reads their actual code and
output and nudges them towards the fix — without handing over the answer.

No sign-up is needed to start. Google sign-in is optional and only syncs
progress across devices.

## How it works

- **Content is code.** Every exercise lives in `content/<track>/…` as a
  Markdown statement, starter code, reference solution, black-box test cases
  and canned hints keyed by mistake pattern. `tutor content validate` runs
  every reference solution against its own tests, so a pull request that adds
  an exercise is known to be correct before anyone reads it.
- **Runs are ephemeral.** Each Run/Submit executes in a fresh sandbox that is
  deleted afterwards. Cost scales with runs, not with students, which is what
  makes "free" sustainable.
- **Hints are cheap by design.** Canned hints answer the common mistakes
  with no model call; an exact-match cache serves repeats; only genuinely new
  situations reach a model, and its reply is checked for leaked solutions
  before a student sees it. A daily budget degrades gracefully to canned
  hints when exhausted.

## Running locally

Requirements: Go 1.26+, Node 22+, python3 on PATH (the local runner uses it).

```sh
cd web && npm ci && npm run build && cd ..
make run
# open http://localhost:8080
```

With no environment set, the server uses an in-memory store, runs student code
with the host's `python3` (no isolation — development only), serves canned
hints only, and hides Google sign-in. Copy `.env.example` to `.env` to switch
each of those on. `deploy/docker-compose.yml` starts Postgres plus the server.

## Configuration

| Variable | Purpose |
|---|---|
| `DATABASE_URL` | Postgres DSN; unset uses the in-memory store |
| `NEEV_API_KEY`, `NEEV_ORG_ID`, `NEEV_PROJECT_ID` | Sandbox API credentials; unset uses the local runner |
| `NEEV_API_BASE`, `NEEV_REGION`, `NEEV_TEMPLATE_ID` | Sandbox API base, region, template (leave template empty for warm starts) |
| `SANDBOX_PREWARM`, `SANDBOX_MAX_INFLIGHT` | Sandboxes kept ready, and the concurrency cap |
| `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` | OpenAI-compatible chat endpoint for model hints |
| `MODEL_DAILY_BUDGET` | Global cap on model calls per day (0 = unlimited) |
| `GOOGLE_CLIENT_ID` | Enables Google sign-in |
| `SESSION_KEY` | ≥32 bytes; signs the session cookie |
| `CONTENT_DIR` | Load exercises from a directory instead of the embedded copy |

## Adding an exercise

1. Create `content/cbse-11/<unit>/<slug>/` with `exercise.md` (front matter:
   `id`, `title`, `concepts`, `syllabus`, `difficulty`), `starter.py`,
   `solution.py`, `tests.yaml`, and optionally `hints.yaml`.
2. Add the id to the unit in `content/cbse-11/track.yaml`.
3. Run `make validate`.

Test cases are `stdin` → expected `stdout`, compared whitespace-tolerant
unless `strict: true`. Mark at least one case visible; hidden cases are never
shown to students or to the model. Hint matchers AND their fields:
`exception`, `message` (regex), `failing_test`, `flag` (one of the harness's
mistake signals such as `input_not_converted`).

## Development

```sh
make lint       # gofmt + vet
make test       # Go tests (+ Postgres conformance if TUTOR_TEST_DATABASE_URL is set) and web tests
make validate   # content checks
make docker     # production image
```

## License

MIT
