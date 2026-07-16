// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  exportTemplateJSON,
  parseTemplateImport,
  templateExportFilename,
  triggerDownload,
} from './templateImportExport'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('templateImportExport', () => {
  it('round-trips exported template JSON through the import parser', () => {
    const json = exportTemplateJSON({
      name: 'My Template',
      phase: 'during',
      sections: [
        { heading: 'One', instruction: 'First' },
        { heading: 'Two', instruction: 'Second' },
      ],
      auto_run: false,
    })

    expect(parseTemplateImport(json)).toEqual({
      name: 'My Template',
      phase: 'during',
      sections: [
        { heading: 'One', instruction: 'First' },
        { heading: 'Two', instruction: 'Second' },
      ],
      auto_run: false,
    })
  })

  it('throws on invalid import shapes', () => {
    expect(() => parseTemplateImport('not json')).toThrow('Not valid JSON')
    expect(() => parseTemplateImport('{}')).toThrow('Template name is missing or invalid')
    expect(() => parseTemplateImport('{"name":" ","sections":[{"heading":"H","instruction":"I"}]}')).toThrow(
      'Template name is missing or invalid',
    )
    expect(() => parseTemplateImport('{"name":"X","phase":"later","sections":[{"heading":"H","instruction":"I"}]}')).toThrow(
      'Invalid phase "later"',
    )
    expect(() => parseTemplateImport('{"name":"X","sections":[]}')).toThrow(
      'Template must have at least one section',
    )
    expect(() => parseTemplateImport('{"name":"X","sections":"nope"}')).toThrow(
      'Template must have at least one section',
    )
    expect(() => parseTemplateImport('{"name":"X","sections":[{"instruction":"I"}]}')).toThrow(
      'Section 1 is missing a heading',
    )
    expect(() => parseTemplateImport('{"name":"X","sections":[{"heading":"H"}]}')).toThrow(
      'Section 1 is missing an instruction',
    )
  })

  it('defaults phase to after and auto_run to true when omitted', () => {
    expect(
      parseTemplateImport('{"name":"X","sections":[{"heading":"H","instruction":"I"}]}'),
    ).toEqual({
      name: 'X',
      phase: 'after',
      sections: [{ heading: 'H', instruction: 'I' }],
      auto_run: true,
    })
  })

  it('derives export filenames from the template name', () => {
    expect(templateExportFilename('My 1:1!')).toBe('my-1-1.json')
    expect(templateExportFilename('   ')).toBe('template.json')
  })

  it('triggers a browser download for exported content', () => {
    const originalCreateObjectURL = URL.createObjectURL
    const originalRevokeObjectURL = URL.revokeObjectURL
    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: vi.fn(() => 'blob:mock-url'),
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      configurable: true,
      value: vi.fn(() => {}),
    })

    const createObjectURL = URL.createObjectURL as unknown as ReturnType<typeof vi.fn>
    const revokeObjectURL = URL.revokeObjectURL as unknown as ReturnType<typeof vi.fn>
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    triggerDownload('x.json', '{}')

    expect(createObjectURL).toHaveBeenCalledTimes(1)
    expect(revokeObjectURL).toHaveBeenCalledTimes(1)
    expect(click).toHaveBeenCalledTimes(1)

    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: originalCreateObjectURL,
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      configurable: true,
      value: originalRevokeObjectURL,
    })
  })
})
