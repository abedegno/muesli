#!/usr/bin/env node

import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const HUES =
  'red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|slate|gray|zinc|neutral|stone'

// Only these utility prefixes make a hue name a real Tailwind colour class.
// Without the prefix, prose and identifiers containing "blue-500" are findings.
const UTILITIES =
  'bg|text|border|ring|outline|fill|stroke|shadow|decoration|divide|accent|caret|placeholder|from|via|to'

const TOKEN_DEFINITION_FILE = 'src/renderer/styles/tokens.css'

export const RULES = [
  {
    id: 'invalid-var-composition',
    pattern: /\b(?:hsla?|rgba?)\(\s*var\(/g,
    message:
      'colour tokens are hex literals; wrapping var(--token) in hsl()/rgb() is invalid CSS and silently falls back',
    appliesTo: () => true,
  },
  {
    id: 'raw-palette-class',
    // black/white have no numeric step, so they are matched separately.
    pattern: new RegExp(`\\b(?:${UTILITIES})-(?:(?:${HUES})-(?:50|[1-9]00)|black|white)\\b`, 'g'),
    message: 'raw Tailwind palette class bypasses the design tokens',
    appliesTo: (file) => /\.tsx?$/.test(file),
  },
  {
    id: 'hex-literal',
    // 3, 4, 6 and 8 digit forms are all valid CSS colours.
    pattern: /#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{4}|[0-9a-fA-F]{3})\b/g,
    message: 'hex colour literal outside the token definitions; define it as a token in tokens.css',
    appliesTo: () => true,
  },
]

// Note this deliberately excludes fixed-height-virtualisation, which is added
// by scanFixedHeightVirtualisation rather than by RULES. validateExceptions
// therefore rejects any exception for it as malformed -- which is intended: the
// design requires that rule to be measured or banned, never deferred.
const RULE_IDS = new Set(RULES.map((r) => r.id))

function normalise(file) {
  const posix = file.split(path.sep).join('/')
  const marker = posix.indexOf('src/renderer/')
  return marker === -1 ? posix : posix.slice(marker)
}

function isScannable(file) {
  return !/\.(test|spec)\.[jt]sx?$/.test(file)
}

export function validateExceptions(exceptions, now = new Date()) {
  const violations = []
  if (!Array.isArray(exceptions)) {
    return [
      {
        file: 'scripts/token-compliance-exceptions.json',
        line: 1,
        rule: 'malformed-exception',
        message: 'the exception file must contain a JSON array',
        snippet: '',
      },
    ]
  }

  for (const e of exceptions) {
    const where = {
      file: typeof e?.file === 'string' ? e.file : 'scripts/token-compliance-exceptions.json',
      line: 1,
      snippet: JSON.stringify(e ?? null).slice(0, 120),
    }
    const expiry = new Date(e?.expires ?? '')
    const malformed =
      typeof e?.file !== 'string' ||
      !RULE_IDS.has(e?.rule) ||
      typeof e?.reason !== 'string' ||
      e.reason.trim() === '' ||
      typeof e?.owner !== 'string' ||
      e.owner.trim() === '' ||
      !Number.isInteger(e?.count) ||
      e.count < 0 ||
      Number.isNaN(expiry.getTime())

    if (malformed) {
      violations.push({
        ...where,
        rule: 'malformed-exception',
        message:
          'each exception needs file, a known rule, a non-empty reason and owner, an integer count and a parseable expires date',
      })
      continue
    }
    if (expiry <= now) {
      violations.push({
        ...where,
        rule: 'expired-exception',
        message: `exception for ${e.rule} expired on ${e.expires} (owner: ${e.owner})`,
      })
    }
  }
  return violations
}

export function scanSource(filePath, source, exceptions = [], now = new Date()) {
  const file = normalise(filePath)
  if (!isScannable(file)) return []

  const lines = source.split('\n')
  const violations = []

  for (const rule of RULES) {
    if (file === TOKEN_DEFINITION_FILE) continue
    if (!rule.appliesTo(file)) continue

    const hits = []
    lines.forEach((line, index) => {
      // matchAll clones the regex internally, so it does not depend on --
      // and does not disturb -- rule.pattern.lastIndex. Each match on the
      // line is its own hit: a line carrying three matches is three
      // occurrences, not one, or a regression added to an already-violating
      // line would never move hits.length.
      for (const _match of line.matchAll(rule.pattern)) {
        hits.push({ line: index + 1, snippet: line.trim() })
      }
    })

    const exception = exceptions.find((e) => normalise(e.file) === file && e.rule === rule.id)
    if (!exception) {
      for (const hit of hits) {
        violations.push({
          file,
          line: hit.line,
          rule: rule.id,
          message: rule.message,
          snippet: hit.snippet,
        })
      }
      continue
    }

    // Expiry and shape are validated separately by validateExceptions.
    if (hits.length > exception.count) {
      violations.push({
        file,
        line: hits[exception.count].line,
        rule: 'exception-count-exceeded',
        message: `${hits.length} ${rule.id} occurrences but the recorded baseline allows ${exception.count}`,
        snippet: hits[exception.count].snippet,
      })
    } else if (hits.length < exception.count) {
      violations.push({
        file,
        line: 1,
        rule: 'exception-count-stale',
        message: `${hits.length} ${rule.id} occurrences but the baseline still allows ${exception.count}; lower it so the ratchet tightens`,
        snippet: '',
      })
    }
  }

  return violations
}

export async function loadExceptions(file) {
  try {
    return JSON.parse(await readFile(file, 'utf8'))
  } catch (error) {
    if (error.code === 'ENOENT') return []
    throw error
  }
}

async function collectFiles(dir) {
  const out = []
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const entryPath = path.join(dir, entry.name)
    if (entry.isDirectory()) out.push(...(await collectFiles(entryPath)))
    else if (/\.(tsx?|css)$/.test(entry.name)) out.push(entryPath)
  }
  return out
}

async function main() {
  const exceptionsPath = 'scripts/token-compliance-exceptions.json'
  const exceptions = await loadExceptions(exceptionsPath)
  const now = new Date()

  const violations = validateExceptions(exceptions, now)
  for (const file of await collectFiles('src/renderer')) {
    violations.push(...scanSource(file, await readFile(file, 'utf8'), exceptions, now))
  }

  if (violations.length === 0) {
    console.log('token compliance: clean')
    return
  }
  for (const v of violations) {
    console.error(`${v.file}:${v.line}  [${v.rule}]  ${v.message}\n    ${v.snippet}`)
  }
  console.error(`\ntoken compliance: ${violations.length} violation(s)`)
  console.error(
    `Fix them, or update ${exceptionsPath} with a reason, an owner, an expiry and a count.`
  )
  process.exitCode = 1
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) await main()
