import AxeBuilder from '@axe-core/playwright'
import { expect, type Page } from '@playwright/test'
import {
  checkA11yExceptions,
  exemptRuleIds,
  type A11yException,
} from '../../src/shared/a11yExceptions'

export type { A11yException }

/**
 * Assert a live, navigated page has no serious accessibility violations,
 * other than ones covered by a scoped, dated `A11yException` (see
 * src/shared/a11yExceptions.ts). A violation whose rule is exempted is not a
 * failure; an exception that is malformed or has expired IS a failure in its
 * own right (named by rule, owner and expiry), regardless of whether the
 * violation it names currently occurs -- this is the same ratchet
 * scripts/check-token-compliance.mjs uses, so recorded debt cannot quietly
 * become permanent.
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

  const serious = results.violations.filter(
    (v) => (v.impact === 'serious' || v.impact === 'critical') && !exemptRules.has(v.id)
  )

  const summary = [
    ...exceptionProblems.map((p) => p.message),
    ...serious.map(
      (v) => `${v.id} (${v.impact}) x${v.nodes.length}: ${v.help}\n    ${v.nodes[0]?.html ?? ''}`
    ),
  ].join('\n')

  expect(
    exceptionProblems.length === 0 && serious.length === 0,
    `${context} has accessibility problems:\n${summary}`
  ).toBe(true)
}
