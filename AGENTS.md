# Working in this repo

Read this before designing or debugging. Each item is here because ignoring it has produced real defects, not as general advice.

## muesli runs in two shapes

The same code runs **locally** on one person's device (Electron, embedded server) and **hosted** as a multi-user platform for a team. Neither is the default case; both are the product.

Design for both, and say which you are reasoning about. Anything that quietly assumes a single user, a single session, or one machine's resources is wrong for half the deployments — and the hosted half is where it fails silently under load rather than on your laptop.

Concretely: session state must be per-session, not package-level; anything unbounded needs a cap; and "fine on a laptop" is not evidence about a server running twenty concurrent meetings.

## Look for prior art first

Before designing something non-trivial, check whether it already exists — in this repo, in a sibling project, or in the dependency you are about to build on top of.

The reflex to check is: *has this problem already been solved by someone whose constraints resemble mine?* Read their interface even if you do not adopt it. A design that has survived contact with users encodes decisions you will otherwise rediscover slowly.

## Do not assume an interface — read it

Check the reference documentation, then the source. Guessing an API's shape, a config key's name, or a tool's accepted flags produces code that looks right, passes review, and fails at runtime.

Two live examples from this codebase: `git revert` was passed a `-q` flag it does not accept, so **every** automatic revert failed for months while four tests asserting the argument *string* stayed green. Separately, a `gh api` call omitted `--paginate`, so it silently read only the first page of results and produced confidently wrong output.

Neither would have survived reading the documentation for the thing being called.

## Debug systematically — do not jump to a cause

When something breaks, gather evidence before proposing a fix. Reproduce it. Read the actual error rather than the first plausible line. Confirm the mechanism, then fix it.

The failure mode is picking a theory that fits one visible symptom and patching that. It produces a change that looks reasonable, may even go green, and leaves the real defect in place — now harder to find because the obvious suspect has been "fixed".

If three attempts have failed, stop fixing and question the design. Three failures usually means the shape is wrong, not that the fourth patch will land.

## Tests must be able to fail

A guard that has never been observed failing is indistinguishable from one that does not work. When you add a check, break the thing it protects, watch it go red, then restore it — and say so in the PR.

Assert on behaviour, not on the shape of a string. Testing that a command's arguments *look* correct says nothing about whether the command accepts them.
