# Working in this repo

Read this before designing or debugging. Each item is here because ignoring it has produced real defects, not as general advice.

## muesli runs in two shapes

The same code runs **locally** on one person's device (Electron, embedded server) and **hosted** as a multi-user platform for a team. Neither is the default case; both are the product.

Design for both, and say which you are reasoning about. Anything that quietly assumes a single user, a single session, or one machine's resources is wrong for half the deployments — and the hosted half is where it fails silently under load rather than on your laptop.

Concretely: session state must be per-session, not package-level; anything unbounded needs a cap; and "fine on a laptop" is not evidence about a server running twenty concurrent meetings.

## Look for prior art first

Before designing something non-trivial, check whether it already exists — in this repo, in a sibling project, or in the dependency you are about to build on top of.

The reflex to check is: _has this problem already been solved by someone whose constraints resemble mine?_ Read their interface even if you do not adopt it. A design that has survived contact with users encodes decisions you will otherwise rediscover slowly.

## Do not assume an interface — read it

Check the reference documentation, then the source. Guessing an API's shape, a config key's name, or a tool's accepted flags produces code that looks right, passes review, and fails at runtime.

Two live examples from this codebase: `git revert` was passed a `-q` flag it does not accept, so **every** automatic revert failed for months while four tests asserting the argument _string_ stayed green. Separately, a `gh api` call omitted `--paginate`, so it silently read only the first page of results and produced confidently wrong output.

Neither would have survived reading the documentation for the thing being called.

## Debug systematically — do not jump to a cause

When something breaks, gather evidence before proposing a fix. Reproduce it. Read the actual error rather than the first plausible line. Confirm the mechanism, then fix it.

The failure mode is picking a theory that fits one visible symptom and patching that. It produces a change that looks reasonable, may even go green, and leaves the real defect in place — now harder to find because the obvious suspect has been "fixed".

If three attempts have failed, stop fixing and question the design. Three failures usually means the shape is wrong, not that the fourth patch will land.

## A red check is a real failure until proven otherwise

When CI goes red, assume the failure is genuine and read the log. "Probably infrastructure" is a conclusion to be earned, not a starting position. Guessing wrong costs a re-run at best; at worst it hides a real defect behind minutes of silent retrying while the build looks like it is making progress.

Two things this has already caught here. First, `client (node)` runs `prettier --check .` over the **whole repo, markdown included** — so a documentation-only change with no code in it can turn CI red. Run `npx prettier --write` on any markdown you add. Second, an automated recovery path classified that exact prettier failure as infrastructural and re-ran the build rather than reporting it. Defaulting to "infra" on an ambiguous signal is the wrong direction: a real failure retried silently wastes time and buries its cause, whereas an infra blip reported as real is merely visible.

## Tests must be able to fail

A guard that has never been observed failing is indistinguishable from one that does not work. When you add a check, break the thing it protects, watch it go red, then restore it — and say so in the PR.

Assert on behaviour, not on the shape of a string. Testing that a command's arguments _look_ correct says nothing about whether the command accepts them.

This applies to throwaway verification at the terminal, not only to committed tests. `pgrep -f "some-task"` matches any command line containing that string — including the shell you just typed it into — so it reports the task as running whether or not it is. The same pattern given to `pkill -f` kills the invoking shell. Both happened here in a single command: the `pkill` stopped the work before it started, and the `pgrep` then confirmed it was running.

## Build fixtures from real data, not from its assumed shape

A test is only as good as the sample it runs on. Hand-written fixtures encode what you _believe_ the data looks like, and that belief is exactly what the test was supposed to check — so a wrong one produces a test that passes, reads correctly in review, and asserts nothing.

Take one real sample first: a captured API response, actual command output, a row from the table. Then write the fixture from it.

Three of these landed in a single afternoon in this project's tooling, all of them plausible on the page:

- A fixture used `success`/`failure` where the function under test consumed `pass`/`fail`. Nothing matched, the verdict fell through to the default, and one assertion passed for entirely the wrong reason.
- A fake `gh` had a catch-all branch that answered every unrecognised subcommand with the same string. A newly added call then received that string as if it were a list of required checks.
- A fixture meant to prove exact-vs-substring matching contained none of the values it was matching against, making it indistinguishable from the separate case where nothing matches at all.

The first two were caught only by running the suite; the third only by a later change colliding with it. None would have been caught by reading.
