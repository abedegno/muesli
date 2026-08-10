import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// @ts-expect-error The production checker is intentionally a directly executable JavaScript module.
import { checkAnnotations } from '../scripts/check-e2e-defect-annotations.mjs'

const fixtures = resolve(__dirname, 'fixtures/e2e-defect-annotations')

describe('e2e defect annotation checker', () => {
  it('accepts test.fail for an open issue', async () => {
    const result = await checkAnnotations(resolve(fixtures, 'open-fail'), async () => 'open')
    expect(result).toEqual({ failures: [], warnings: [], exitCode: 0 })
  })

  it('rejects test.fail for a closed issue', async () => {
    const result = await checkAnnotations(resolve(fixtures, 'closed-fail'), async () => 'closed')
    expect(result.exitCode).toBe(1)
    expect(result.failures[0]).toMatchObject({ line: 2, issue: 702, kind: 'fail' })
    expect(result.failures[0].file).toContain('closed-fail/regression.spec.ts')
  })

  it('warns without failing for test.skip on a closed issue', async () => {
    const result = await checkAnnotations(resolve(fixtures, 'closed-skip'), async () => 'closed')
    expect(result.exitCode).toBe(0)
    expect(result.failures).toEqual([])
    expect(result.warnings[0]).toMatchObject({ line: 2, issue: 703, kind: 'skip' })
  })

  it('rejects an annotation without an issue reference', async () => {
    const result = await checkAnnotations(resolve(fixtures, 'unattributed'), async () => 'open')
    expect(result.exitCode).toBe(1)
    expect(result.failures[0]).toMatchObject({
      line: 2,
      issue: null,
      kind: 'fail',
      message: 'no issue reference found',
    })
  })

  it('passes cleanly when there are no annotations', async () => {
    const lookup = vi.fn(async () => 'open')
    const result = await checkAnnotations(resolve(fixtures, 'none'), lookup)
    expect(result).toEqual({ failures: [], warnings: [], exitCode: 0 })
    expect(lookup).not.toHaveBeenCalled()
  })
})
