# Nightly desktop build — design

**Date:** 2026-08-01
**Status:** Approved design (brainstorm complete) — next: implementation plan
**Repo:** `abedegno/muesli` (`.github/workflows/desktop-release.yml`)

## Problem

Breakage in the packaged desktop app is currently discovered **at release time**, when it is most expensive.

On 2026-07-31, PR #499 merged with its desktop build red. The same defect then failed the `desktop-v0.1.14` tag build after the app had been built, signed and notarized, so no macOS download was published and a version number was spent. The identical failure had been visible on the PR **four minutes before it merged**.

### Why the PR signal did not stop it

Investigated 2026-08-01. Evidence:

- Every check on PR #499's head registered at **23:51:21–23:51:29**, including `build` and `build-linux` — they were never missing from the check list.
- `build` failed at **23:58:38**; the PR merged at **00:02:45**, four minutes later.
- `_normalize_ci` maps `fail|cancel` → red correctly, so this is not a mapping bug.
- **`merge_ready_pr` never consults CI.** It waits for `gh pr view --json mergeable` to report `MERGEABLE` — GitHub's _conflict_ status, not CI — posts `bircher/cross-review=success`, and merges.
- `_poll_ci` exists but runs only in recovery paths (`_wait_ci`, `_rerun_and_wait_ci`, ground-truth recovery), never on the happy path.
- Branch protection requires only `server (go)`, `client (node)`, `bircher/cross-review`. All three were green.

**Root cause:** Bircher delegates CI enforcement entirely to branch protection. Any check that is not _required_ is invisible to the merge decision — by construction, today and for anything added later.

An earlier hypothesis — that Bircher took a green verdict from a check set that did not yet include the desktop build — was **tested and refuted** by the registration timestamps above.

## Decision

Add a **nightly** build of `main` that exercises the full packaging path and the packaged-app smokes.

This is deliberately **detection, not prevention**. Making the desktop build a required status check is the only mechanism that would stop a bad merge, and it was considered and declined: it would add ~7 minutes to every Bircher item touching `src/**`, and the job has a flake history (the D-Bus failure on `desktop-v0.1.13`) that could stall an autonomous wave. Throughput was preferred over prevention, accepting that bad merges still land and sit on `main` for up to a day.

Timing supports the trade-off: the smokes themselves cost **34 seconds**; the expense is producing the app (build + sign 4m58s, notarize + staple 1m53s, whisper libs 1m09s, Go binaries 50s).

## Design

### 1. Nightly trigger

Add a `schedule:` trigger to `desktop-release.yml` so both jobs (`build`, `build-linux`) run against `main`, exercising the same packaging path a release uses. `workflow_dispatch` remains for on-demand runs.

Use **`cron: '17 5 * * *'`** — clear of the existing nightlies (`streaming-nightly` at `17 3 * * *`, `image-publish` at `17 4 * * *`) so the three do not contend for runners, and late enough that an overnight Bircher wave has normally finished merging.

### 2. Unify how "is this a release?" is expressed

The workflow currently expresses that question **two different ways**, equivalent only because there are just two triggers:

| Line   | Predicate                                       | Purpose                      |
| ------ | ----------------------------------------------- | ---------------------------- |
| `:102` | `event_name == 'pull_request'` → skip notarize  | build + sign                 |
| `:109` | `event_name != 'pull_request'`                  | notarize + staple dmg        |
| `:135` | `event_name != 'pull_request'`                  | `stapler validate` app + dmg |
| `:230` | `startsWith(github.ref, 'refs/tags/desktop-v')` | attach dmg to release        |
| `:337` | `startsWith(github.ref, 'refs/tags/desktop-v')` | attach AppImage to release   |

A scheduled run is `event_name = schedule` (**not** a pull request) on `refs/heads/main` (**not** a tag), so it satisfies the first idiom but not the second.

**Change the three `event_name != 'pull_request'` conditions to the tag test already used at `:230`/`:337`,** so the release predicate is expressed one way everywhere.

This is the substance of the change, not tidying:

- Adding `schedule:` and changing nothing else makes the nightly take the _else_ branch at `:102` and `:109` and **notarize every night** — slow, and it spends Apple notarization submissions on an artifact that is discarded.
- The obvious partial fix is worse: skipping notarization at `:109` while leaving `:135` means `stapler validate` runs against an unstapled app and **the nightly fails every single night**, which trains everyone to ignore it.

Signing remains enabled for scheduled runs: it is part of the packaging path under test, and the smokes launch the resulting app.

### 3. Publishing

No change required. Both release-attach steps are already gated on the tag predicate, so a scheduled run skips them. The implementation must **verify** this rather than assume it — a nightly that publishes a release would be a serious regression.

### 4. Failure visibility

Rely on GitHub's built-in notification for failed scheduled runs. No issue-filing automation in this change: a nightly that nobody reads is worthless, but the cheapest thing that could work should be tried before building machinery. If it proves too quiet in practice, revisit.

## Success criteria

- A scheduled run builds and smokes `main` on both platforms nightly.
- A scheduled run does **not** notarize, does **not** staple, does **not** run `stapler validate`, and does **not** attach anything to a release.
- Tag builds are byte-for-byte unchanged in behaviour: still sign, notarize, staple, validate and publish.
- PR builds are unchanged: still sign, still skip notarization.
- A breakage merged to `main` is visible within a day rather than at the next release.

## Out of scope

- Changing required status checks or branch protection.
- Changing PR trigger behaviour.
- Changing Bircher's merge logic (`merge_ready_pr` still will not consult CI; that is a known, accepted gap recorded here).
- Automated issue filing on nightly failure.

## Known residual risk

Bad merges still land on `main` and remain until the nightly runs. This design bounds the blast radius; it does not prevent it. If that proves insufficient, the escalation is to make the desktop build a required check — accepting the wave cost that was declined here.
