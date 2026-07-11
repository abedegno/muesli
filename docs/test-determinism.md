# Test Determinism Gate

This document describes the test-determinism gate: what is banned, the approved
alternatives, and how to manage exemptions.

## Why determinism matters

Flaky tests that depend on real time or real network I/O are hard to debug,
slow to run, and give false confidence. The gate enforces two rules:

1. **No `time.Now()` or `time.Sleep()` in Go unit tests** — production code
   that calls `time.Now()` internally can still use it, but test code must not.
2. **No raw outbound HTTP in Python plugin tests** — all httpx calls must be
   intercepted by `respx`.

## Banned patterns and approved alternatives

### Go

| Banned                           | Approved alternative                            |
| -------------------------------- | ----------------------------------------------- |
| `time.Now()` in `*_test.go`      | `testutil.FakeClock.Now()`                      |
| `time.Sleep(...)` in `*_test.go` | Advance `FakeClock` + synchronise with channels |

#### Using FakeClock

```go
import (
    "testing"
    "time"

    "github.com/abedegno/muesli/internal/testutil"
)

func TestSomething(t *testing.T) {
    clk := testutil.NewFakeClock(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC))
    // pass clk.Now to the system under test
    clk.Advance(5 * time.Minute)
    // assert on clk.Now() instead of time.Now()
}
```

### Python plugins

| Banned                             | Approved alternative                                    |
| ---------------------------------- | ------------------------------------------------------- |
| `datetime.datetime.now()` in tests | `frozen_clock` fixture                                  |
| `time.sleep(...)` in tests         | Advance `frozen_clock` / restructure test               |
| Raw `httpx` calls without mocking  | `respx` routes (provided by `_block_real_http` autouse) |

#### Using frozen_clock

```python
def test_something(frozen_clock):
    # datetime.now() is frozen at 2024-01-15 12:00:00
    result = my_function_that_uses_now()
    assert result.timestamp == frozen_clock
```

#### Using _block_real_http

The `_block_real_http` autouse fixture is active in every test automatically.
To mock an outbound call, use `respx`:

```python
import respx
import httpx

def test_something():
    respx.get("http://example.com/api").mock(return_value=httpx.Response(200, json={"ok": True}))
    # code under test can call httpx.get("http://example.com/api") and will get the mock
```

If a test legitimately needs to reach a **local** server (e.g. `served_audio_url`),
add a pass-through route before yielding the URL:

```python
respx.get(url).pass_through()
```

## Go gate: adding a skip exemption

The gate script (`scripts/check-test-determinism.sh`) reads exemptions from
`scripts/check-test-determinism-skip.txt`. Each non-comment, non-blank line is
a relative path (from the repo root) to a `*_test.go` file that is exempt.

**Rules:**

- Only add an entry alongside a migration PR that tracks the removal.
- Never add new production test files to the skip list without a companion issue.

Example skip list entry:

```
# Pending migration — see issue #42
internal/store/notes_test.go
```

## CI integration

### Go server job

The `check-test-determinism` make target runs before `go test`:

```yaml
- run: make check-test-determinism
- run: go test ./... -p 1 -parallel 2
```

### Python plugins job

A `ruff check --select TID251` step runs before `pytest` for each plugin:

```yaml
- name: Ruff TID gate (${{ matrix.dir }})
  working-directory: plugins/${{ matrix.dir }}
  run: |
    pip install 'ruff>=0.4'
    ruff check tests/ --select TID251
- name: Test (${{ matrix.dir }})
  working-directory: plugins/${{ matrix.dir }}
  run: pytest
```
