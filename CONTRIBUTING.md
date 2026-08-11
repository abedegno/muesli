# Contributing to Muesli

Thanks for your interest in Muesli — a privacy-focused, self-hostable
meeting-notes app. This guide covers how to get a dev environment running, the
conventions we follow, and how to land a change.

By participating you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

## Ways to contribute

- **Report bugs** and **request features** via GitHub issues (templates provided).
- **Improve docs** — corrections to [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md),
  the README, or inline comments are very welcome.
- **Write a plugin** — a transcriber or agent in any language that speaks the
  [plugin contract](docs/ARCHITECTURE.md#the-plugin-contract).
- **Pick up backlog work** — see [`ROADMAP.md`](ROADMAP.md) for what's planned.

For anything non-trivial, **open an issue first** so we can agree on the approach
before you invest time. This is especially true for changes to the plugin
contract or the data model.

## Getting oriented

Read [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) before diving in — it maps
the components, the processing pipeline, and where each responsibility lives. The
design spec explains the
_why_ behind the architecture.

## Development environment

You'll need: **Go 1.25+**, **Node 22+**, **Docker** (or colima), and **Python
3.11+** if you're touching the reference plugins.

### Run the whole stack

The fastest way to a working system — Postgres, Ollama, the Whisper transcriber,
the agent, and the server, all wired together:

```bash
docker compose up
```

Open <http://localhost:8080/admin>, create an account, and the default plugins
are already registered. First boot is slow (model downloads). No `.env` is
required; copy `.env.example` to `.env` to override anything.

### Work on the Go server

```bash
colima start                # or Docker Desktop
make test-db                # throwaway Postgres on :5433 (prints TEST_DATABASE_URL)
export TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5433/muesli_test?sslmode=disable
make test                   # full Go suite — serialized with -p 1 (see note below)
make build                  # build admin SPA, embed it, compile the binary
make run                    # run the server directly
make test-db-stop           # tear down the test DB
```

> **Why `-p 1`?** DB-backed tests share one test database and truncate between
> tests, so package test binaries must run serially to avoid cross-package
> `TRUNCATE` races. Don't remove it.

### Work on the admin UI

```bash
cd web/admin
npm ci
npm run dev          # Vite dev server with hot reload
npm test             # Vitest
npm run build        # emits into internal/adminui/dist (embedded by the Go build)
```

> Note: `npm run build` uses `emptyOutDir`, which wipes the tracked placeholder
> fixtures in `internal/adminui/dist/`. If you build locally, restore them with
> `git checkout internal/adminui/dist` (CI rebuilds them, so it's harmless).

### Work on the desktop client

```bash
npm install
npm test             # Vitest
npm run dev          # launch Electron against http://localhost:8080
```

#### Testing the packaged app on macOS

As general macOS behavior, TCC privacy permissions such as microphone, camera,
and screen recording are attributed to the process that actually launches the
app. Starting a binary directly from a shell attributes its permission requests
to the terminal rather than to the app itself. For UI or capture testing, launch
the packaged app through LaunchServices instead of executing
`Muesli.app/Contents/MacOS/Muesli` directly:

```bash
open -a /Applications/Muesli.app --args --remote-debugging-port=9333
```

If TCC-gated capture behaves unexpectedly, check System Settings → Privacy &
Security → Microphone (or the relevant permission). Seeing the terminal app
listed instead of Muesli confirms that the app was launched with terminal
attribution.

### Work on a plugin

Each plugin has its own virtualenv (gitignored):

```bash
cd plugins/whisper-transcriber
python -m venv .venv && .venv/bin/pip install -e '.[dev]'
.venv/bin/python -m pytest tests/
```

**Validate any plugin against the contract** with the conformance suite:

```bash
cd plugins/conformance
.venv/bin/python -m pytest tests/
# and run the CLI against a running plugin to self-certify it
```

## Testing plugins

Any plugin — including third-party ones built outside this repository — can be
validated against the Muesli contract using the conformance suite. Running the
suite before registering a plugin catches contract violations early and gives you
a clear pass/fail signal with per-check detail.

See [`plugins/conformance/README.md`](plugins/conformance/README.md) for full
instructions.

## Conventions

- **Test-driven.** Write the failing test first, make it pass, then refactor.
  Every behavioral change ships with a test. PRs without tests will be asked for
  them.
- **Match the surrounding code.** Follow existing patterns, naming, and comment
  density in the file you're editing. Keep files focused — one clear
  responsibility each.
- **Go:** `gofmt`/`go vet` clean. Keep SQL in `internal/store` and migrations;
  don't scatter queries across packages.
- **TypeScript:** the project is typed; keep it that way (no `any` escape hatches
  without reason).
- **DRY, YAGNI.** Don't build for requirements that aren't here yet — capture
  future ideas in `ROADMAP.md` instead.
- **Migrations are append-only and timestamp-versioned.** Create new migrations
  with `make new-migration name=<snake_name>` (or `scripts/new-migration.sh`),
  which stamps a collision-free `YYYYMMDDHHMMSS_<name>.{up,down}.sql`. Never
  hand-pick a number and never edit a shipped migration. (Existing `0001`-`00NN`
  files predate this and stay as-is; they sort before any timestamp.)
- **Don't break the plugin contract** without discussion — it's a public
  interface other people's plugins depend on. Contract or data-model changes
  need an issue first.

## Commits and pull requests

- Keep commits focused and the message clear about _what_ and _why_. We loosely
  follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`) — not enforced, but
  appreciated.
- Branch off `main`. Rebase rather than merge `main` into your branch when you
  can keep history clean.
- Before opening a PR: **tests pass** (`make test` and any relevant `npm test` /
  `pytest`), and the build succeeds (`make build`).
- Open the PR against `main`, fill in the template, and link the issue it
  addresses. Describe how you tested it.
- A maintainer will review. Expect a round or two of feedback — that's normal.

### Branch protection (main)

`main` requires both CI checks green before merge (enforced for admins too), so a
red PR can't land. Applied once by a maintainer with admin rights:

```bash
gh api -X PUT repos/abedegno/muesli/branches/main/protection --input - <<'JSON'
{
  "required_status_checks": { "strict": false, "contexts": ["server (go)", "client (node)"] },
  "enforce_admins": true,
  "required_pull_request_reviews": { "required_approving_review_count": 0 },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
JSON
```

`strict:false` = no forced rebase-before-merge. Emergency override (rare): re-run
the same call with `"enforce_admins": false`, merge, then restore `true`.

#### The `e2e-desktop` check

`e2e-desktop` runs the Playwright end-to-end suite against the desktop app in embedded mode
on Linux — a real Go server, real embedded Postgres, and fake plugins substituted for
whisper and the agent so results are deterministic. It takes 2-3 minutes.

**Some of its specs are marked `test.fail()` on purpose.** They are regression tests for
defects that are still open: they assert that the bug _reproduces_. Playwright treats an
expected failure as a pass, so CI stays green while the defect exists.

The consequence matters if you are fixing one of those defects: **the moment your fix
works, that spec starts passing, Playwright reports "expected failure that passed", and
`e2e-desktop` goes red.** That is deliberate — it is what stops a fixed bug leaving a stale
expected-failure behind. Remove the `test.fail(...)` line **in the same PR as your fix**.

Each annotation names the issue it belongs to, so search the spec for the issue number you
are fixing.

#### The `review-gate` check

Branch protection also requires the `review-gate` status check. It answers one
question: has this PR been independently reviewed, or is it explicitly exempt?

The autonomous Bircher pipeline posts `bircher/cross-review`, but that status has
only one producer. Requiring it directly would mean that only Bircher's own
pipeline could ever make a PR mergeable, preventing human contributors from
landing changes. `review-gate` is required instead because it can recognize both
an independent review and an explicit exemption.

For a human-authored PR, the check will initially show
`pending — awaiting bircher/cross-review`. This means the PR still needs a
maintainer to route it through independent review. If your PR is stuck on this
check, ask a maintainer to arrange that review; you cannot clear it yourself.

Currently, only `dependabot[bot]` is exempt. Exemptions are an allowlist by
design, not a denylist: wrongly requiring review is a visible nuisance, while
wrongly exempting an author could silently let unreviewed code merge. Any
unrecognized author therefore falls through to requiring review rather than
bypassing it.

## Licensing of contributions

Muesli is licensed under the **GNU Affero General Public License v3.0**
([LICENSE](LICENSE)). By submitting a contribution, you agree that it is licensed
under the same terms. Don't paste in code you don't have the right to relicense
under the AGPL.

## Security

**Do not** open a public issue for security vulnerabilities. See
[SECURITY.md](SECURITY.md) for how to report them privately.

## Questions

Open a [discussion or issue](../../issues). We're happy to help you find a good
first task.
