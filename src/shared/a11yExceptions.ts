/**
 * Scoped, dated exceptions for the accessibility floor (e2e/helpers/a11y.ts),
 * mirroring the philosophy of scripts/check-token-compliance.mjs: a known
 * violation that needs a design decision is recorded with an owner, a reason
 * and an expiry rather than silenced by weakening the scan itself. An
 * exception that is malformed or has expired is treated as a failure in its
 * own right, so the list cannot quietly become a permanent exemption.
 */
export type A11yException = {
  /** The axe rule id the exception covers, e.g. 'color-contrast'. */
  rule: string
  reason: string
  owner: string
  /** ISO date string. The exception stops applying at (and including) this date. */
  expires: string
  /**
   * How many violating nodes the exception covers, summed across every axe
   * violation carrying this rule id. This is what makes the entry a ratchet
   * rather than a blanket suppression: exempting the rule outright would hide
   * every future regression of the highest-frequency rule axe has, and the
   * floor would be decorative for as long as the exception lived. A
   * non-integer or negative count is malformed, like any other missing field.
   */
  count: number
}

/** Observed violating-node counts for one axe run, one entry per rule id. */
export type A11yNodeCount = {
  rule: string
  nodes: number
}

export type A11yExceptionProblem = {
  exception: A11yException
  message: string
}

const DAY_MS = 24 * 60 * 60 * 1000

/**
 * How long before an exception expires the floor starts saying so.
 *
 * This exception and the Plating Stage 3 entries in
 * scripts/token-compliance-exceptions.json all expire on one date. Without a
 * warning window that is a cliff: on that morning every open PR fails both
 * `client (node)` and `e2e-desktop` at once with no notice. The dating is
 * deliberate and is NOT moved by this -- the warning opens a month in which to
 * renew the entry or pay the debt down.
 */
export const A11Y_EXPIRY_WARNING_DAYS = 30

/**
 * Validates a list of accessibility exceptions against a clock, returning one
 * problem per malformed or expired entry. Every exception is checked
 * unconditionally -- independent of whether the rule it names currently
 * appears in an axe scan -- exactly like validateExceptions() in
 * scripts/check-token-compliance.mjs, so debt can't sit unmonitored past its
 * expiry just because the underlying violation happens not to have run
 * recently. `now` is a parameter, not read internally, so the expiry ratchet
 * can be tested without waiting on the calendar.
 */
export function checkA11yExceptions(
  exceptions: A11yException[],
  now: Date
): A11yExceptionProblem[] {
  const problems: A11yExceptionProblem[] = []

  for (const exception of exceptions) {
    const expiry = new Date(exception?.expires ?? '')
    const malformed =
      typeof exception?.rule !== 'string' ||
      exception.rule.trim() === '' ||
      typeof exception?.reason !== 'string' ||
      exception.reason.trim() === '' ||
      typeof exception?.owner !== 'string' ||
      exception.owner.trim() === '' ||
      !Number.isInteger(exception?.count) ||
      exception.count < 0 ||
      Number.isNaN(expiry.getTime())

    if (malformed) {
      problems.push({
        exception,
        message: `malformed a11y exception ${JSON.stringify(exception)}: needs a non-empty rule, reason and owner, a non-negative integer count, and a parseable expires date`,
      })
      continue
    }

    if (expiry.getTime() <= now.getTime()) {
      problems.push({
        exception,
        message: `a11y exception for ${exception.rule} expired on ${exception.expires} (owner: ${exception.owner})`,
      })
    }
  }

  return problems
}

/**
 * The set of axe rule ids covered by a currently-valid (non-malformed,
 * unexpired) exception. A rule with a problematic exception is deliberately
 * NOT exempt -- an expired or malformed exception protects nothing, so the
 * violation it was meant to cover still fails, alongside the exception
 * problem itself.
 */
export function exemptRuleIds(exceptions: A11yException[], now: Date): Set<string> {
  return new Set(usableExceptions(exceptions, now).map((e) => e.rule))
}

/** The exceptions that are well-formed and unexpired, and so actually apply. */
function usableExceptions(exceptions: A11yException[], now: Date): A11yException[] {
  const problematic = new Set(checkA11yExceptions(exceptions, now).map((p) => p.exception))
  return exceptions.filter((e) => !problematic.has(e))
}

/**
 * Reports every exempted rule whose observed violating nodes outnumber the
 * count its exception recorded.
 *
 * This is the direct port of the occurrence ratchet in
 * scripts/check-token-compliance.mjs, and it exists for the same reason: an
 * exception that merely names a rule suppresses every future regression of
 * that rule as well as the debt it was written for. color-contrast is the
 * highest-frequency rule axe has, so exempting it by name alone would remove
 * most of the floor's value until the expiry date.
 *
 * Only the excess direction is enforced. The scanner also reports a baseline
 * that has drifted ABOVE reality, because a file's occurrence count is exact;
 * a rendered node count is not, since fonts, window size and platform
 * rendering all move it. Failing on a count that came in under the baseline
 * would make the floor flaky in the one direction that is an improvement.
 */
export function exceededA11yExceptions(
  observed: A11yNodeCount[],
  exceptions: A11yException[],
  now: Date
): A11yExceptionProblem[] {
  const problems: A11yExceptionProblem[] = []

  for (const exception of usableExceptions(exceptions, now)) {
    const nodes = observed
      .filter((o) => o.rule === exception.rule)
      .reduce((total, o) => total + o.nodes, 0)
    if (nodes > exception.count) {
      problems.push({
        exception,
        message: `a11y exception for ${exception.rule} covers ${exception.count} node(s) but ${nodes} were found (owner: ${exception.owner}); fix the new ones or raise the recorded count with a reason`,
      })
    }
  }

  return problems
}

/**
 * Exceptions that are still valid but expire within `withinDays`.
 *
 * Deliberately separate from checkA11yExceptions: these are warnings the
 * caller surfaces WITHOUT failing the test, or the cliff would just move 30
 * days earlier. Malformed and already-expired entries are skipped -- those are
 * hard failures already, and naming the same debt twice helps nobody.
 */
export function expiringA11yExceptions(
  exceptions: A11yException[],
  now: Date,
  withinDays: number = A11Y_EXPIRY_WARNING_DAYS
): A11yExceptionProblem[] {
  const problems: A11yExceptionProblem[] = []

  for (const exception of usableExceptions(exceptions, now)) {
    const remainingMs = new Date(exception.expires).getTime() - now.getTime()
    if (remainingMs > withinDays * DAY_MS) continue

    const daysLeft = Math.ceil(remainingMs / DAY_MS)
    problems.push({
      exception,
      message: `a11y exception for ${exception.rule} expires in ${daysLeft} day(s), on ${exception.expires} (owner: ${exception.owner}); renew it or clear the debt before it starts failing e2e-desktop`,
    })
  }

  return problems
}
