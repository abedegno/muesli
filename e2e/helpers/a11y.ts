import AxeBuilder from '@axe-core/playwright'
import { expect, type Page } from '@playwright/test'
import {
  checkA11yExceptions,
  exceededA11yExceptions,
  exemptRuleIds,
  expiringA11yExceptions,
  type A11yException,
} from '../../src/shared/a11yExceptions'

export type { A11yException }

/**
 * Assert a live, navigated page has no serious accessibility violations,
 * other than ones covered by a scoped, dated, COUNTED `A11yException` (see
 * src/shared/a11yExceptions.ts).
 *
 * An exempted rule is suppressed only up to the number of violating nodes its
 * exception recorded; the (count + 1)th node fails. Naming a rule is not
 * enough, and deliberately so: color-contrast is the highest-frequency rule
 * axe has, so a blanket exemption would hide every future contrast regression
 * on the audited screens until the expiry date -- the exact mechanism the
 * Plating Stage 0 design rejects for tokens, where the scanner counts
 * occurrences for the same reason.
 *
 * An exception that is malformed or has expired IS a failure in its own right
 * (named by rule, owner and expiry), regardless of whether the violation it
 * names currently occurs, so recorded debt cannot quietly become permanent.
 * Every suppression is also printed with its observed and recorded counts, so
 * a passing run still says what it let through.
 *
 * Scoped to serious and critical impact deliberately: this is a floor that must
 * be able to stay green. Lighthouse cannot do this job -- it audits a static
 * out/renderer with no server and no data, so it never sees these states.
 */
export async function expectNoAxeViolations(
  page: Page,
  context: string,
  exceptions: A11yException[] = [],
  now: Date = new Date()
): Promise<void> {
  const exceptionProblems = checkA11yExceptions(exceptions, now)
  const exemptRules = exemptRuleIds(exceptions, now)

  // Warned about, never failed on. Every exception on this branch expires on
  // one date, so without notice that date arrives as a cliff: `e2e-desktop`
  // and `client (node)` both go red on every open PR the same morning.
  for (const warning of expiringA11yExceptions(exceptions, now)) {
    console.warn(`[a11y] ${context} warning: ${warning.message}`)
  }

  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    // axe-core's default two-phase run (runPartial + finishRun) opens a blank
    // page via context.newPage() to relay results across frames/origins. Electron's
    // single BrowserWindow context does not support creating a second page --
    // Target.createTarget is not implemented -- so that call fails outright.
    // Legacy mode runs axe.run() directly in the existing page instead, which
    // is the documented workaround for exactly this environment and is safe
    // here because the app has no cross-origin iframes to relay.
    .setLegacyMode(true)
    .analyze()

  const impactful = results.violations.filter(
    (v) => v.impact === 'serious' || v.impact === 'critical'
  )
  const serious = impactful.filter((v) => !exemptRules.has(v.id))

  const exempted = impactful.filter((v) => exemptRules.has(v.id))
  const countProblems = exceededA11yExceptions(
    exempted.map((v) => ({ rule: v.id, nodes: v.nodes.length })),
    exceptions,
    now
  )

  // A passing run reports what it suppressed. A floor that silently swallows
  // findings is indistinguishable from one with nothing to swallow.
  for (const exception of exceptions) {
    const nodes = exempted
      .filter((v) => v.id === exception.rule)
      .reduce((total, v) => total + v.nodes.length, 0)
    console.log(
      `[a11y] ${context}: ${exception.rule} suppressed ${nodes} of a recorded ${exception.count} node(s) (owner: ${exception.owner}, expires: ${exception.expires})`
    )
  }

  const summary = [
    ...exceptionProblems.map((p) => p.message),
    ...countProblems.map((p) => p.message),
    ...serious.map(
      (v) => `${v.id} (${v.impact}) x${v.nodes.length}: ${v.help}\n    ${v.nodes[0]?.html ?? ''}`
    ),
  ].join('\n')

  expect(
    exceptionProblems.length === 0 && countProblems.length === 0 && serious.length === 0,
    `${context} has accessibility problems:\n${summary}`
  ).toBe(true)
}
