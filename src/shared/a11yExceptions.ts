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
}

export type A11yExceptionProblem = {
  exception: A11yException
  message: string
}

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
      Number.isNaN(expiry.getTime())

    if (malformed) {
      problems.push({
        exception,
        message: `malformed a11y exception ${JSON.stringify(exception)}: needs a non-empty rule, reason and owner, and a parseable expires date`,
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
  const problems = checkA11yExceptions(exceptions, now)
  const problematic = new Set(problems.map((p) => p.exception))
  return new Set(exceptions.filter((e) => !problematic.has(e)).map((e) => e.rule))
}
