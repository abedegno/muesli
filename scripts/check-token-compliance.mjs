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

// Strips comment content so a colour or class token mentioned only in prose
// -- or, worse, a `#1234`-shaped issue/PR reference -- never counts as
// applied styling.
//
// A single left-to-right pass that tracks quote state, because `//` inside a
// string is not a comment. Treating it as one is a FALSE NEGATIVE, and a false
// negative here is worse than a missed finding: it silently lowers the
// occurrence count an exception's baseline is ratcheted against, so a real
// regression can be added to an excepted file on the same line as a URL and
// the count never moves.
//
// Still line-level, not a tokenizer: it carries no state between lines, so a
// block comment opened with `/*` that closes on a LATER line has only its
// first line stripped. JSDoc continuation lines are handled by the `*` prefix
// test below, which covers the shape that actually occurs. An unbalanced quote
// (an apostrophe in JSX prose, say) leaves the rest of the line treated as a
// string, so a trailing comment there is scanned rather than stripped -- a
// false positive, which fails loudly, rather than a false negative, which does
// not.
function stripComments(line) {
  const trimmed = line.trimStart()
  // A block-comment continuation line, e.g. the `*` lines of a `/** ... */`
  // JSDoc block, including its closing `*/`.
  if (trimmed.startsWith('*')) return ''

  let code = ''
  // The quote character that opened the string we are inside, or null.
  let quote = null

  for (let i = 0; i < line.length; i++) {
    const char = line[i]

    if (quote !== null) {
      code += char
      if (char === '\\') {
        // An escape consumes the next character, so a \" does not close.
        i++
        if (i < line.length) code += line[i]
      } else if (char === quote) {
        quote = null
      }
      continue
    }

    if (char === "'" || char === '"' || char === '`') {
      quote = char
      code += char
      continue
    }

    // A line comment: everything from `//` to the end of the line.
    if (char === '/' && line[i + 1] === '/') return code

    // A block comment. Unterminated on this line means the rest of the line is
    // comment; the continuation lines are handled by the `*` prefix test.
    if (char === '/' && line[i + 1] === '*') {
      const close = line.indexOf('*/', i + 2)
      if (close === -1) return code
      i = close + 1
      continue
    }

    code += char
  }

  return code
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

const DAY_MS = 24 * 60 * 60 * 1000

/**
 * How long before an exception expires the scanner starts saying so.
 *
 * Every Plating Stage 3 entry in the exception file expires on one date, and
 * the a11y floor's exception expires on the same one. Without a warning
 * window, that is a cliff: on that morning every open PR fails `client (node)`
 * and `e2e-desktop` at once, with no notice that it was coming. The dating is
 * deliberate and is NOT moved by this -- the warning just opens a month in
 * which to renew the entry or pay the debt down.
 */
export const EXPIRY_WARNING_DAYS = 30

/**
 * Exceptions that are still valid but expire within `withinDays`.
 *
 * Deliberately NOT part of validateExceptions: these are warnings and must
 * never fail the build, or the cliff would simply move 30 days earlier.
 * Malformed and already-expired entries are skipped, because those are hard
 * failures from validateExceptions and naming them twice helps nobody.
 */
export function expiringExceptions(exceptions, now = new Date(), withinDays = EXPIRY_WARNING_DAYS) {
  if (!Array.isArray(exceptions)) return []

  const warnings = []
  for (const e of exceptions) {
    if (validateExceptions([e], now).length > 0) continue

    const remainingMs = new Date(e.expires).getTime() - now.getTime()
    if (remainingMs > withinDays * DAY_MS) continue

    const daysLeft = Math.ceil(remainingMs / DAY_MS)
    warnings.push({
      file: e.file,
      rule: e.rule,
      owner: e.owner,
      expires: e.expires,
      daysLeft,
      message: `${e.file} [${e.rule}] expires in ${daysLeft} day(s), on ${e.expires} (owner: ${e.owner}); renew it or clear the debt before it starts failing the build`,
    })
  }
  return warnings
}

// A pixel constant is only dangerous when layout arithmetic derives from it:
// styling can change the rendered height without changing the maths, so rows
// drift, overlap, or become unreachable. Declaration and use are on different
// lines, so this is evaluated across the whole file.
//
// WHAT THIS RULE ACTUALLY DETECTS, precisely, so nobody trusts it further than
// it goes. It fires only on: a SCREAMING_SNAKE_CASE `const` whose name matches
// HEIGHT_DECL below, assigned a bare integer literal, where some LATER line
// puts an arithmetic operator directly adjacent to that same identifier.
//
// It does NOT detect, all verified rather than assumed:
//   - camelCase or lowercase names (`const rowHeight = 44`);
//   - object or prop forms (`{ rowHeight: 44 }`, react-window's `itemSize={44}`);
//   - a declaration and its use on the same line;
//   - indirection through any other binding, which is the common shape:
//     `const h = measured[key] ?? ROW_HEIGHT; top += h` reads clean;
//   - and, most importantly, the mechanism TranscriptView.tsx ships TODAY. Its
//     SEGMENT_ROW_HEIGHT / SPEAKER_ROW_HEIGHT constants still seed accumulating
//     layout offsets, but they reach the arithmetic through a ternary
//     (`... : SEGMENT_ROW_HEIGHT`) with no adjacent operator, so this rule
//     returns [] for that file.
//
// So passing this rule is NOT evidence that a component measures its rows. It
// is evidence that one syntactic shape is absent. The real defect in
// TranscriptView was fixed by measurement (see its useLayoutEffect), not by
// this rule going green -- the rule went green on a rewrite that removed
// operator adjacency. Widening it to detect the mechanism rather than the
// syntax is its own piece of work; until then, read it as a lint for one
// obvious spelling of the bug.
const HEIGHT_DECL = /const\s+([A-Z0-9_]*(?:ROW|ITEM|SEGMENT|SPEAKER)[A-Z0-9_]*HEIGHT)\s*=\s*\d+/g

export function scanFixedHeightVirtualisation(file, source) {
  if (!/\.tsx?$/.test(file)) return []
  const violations = []
  const lines = source.split('\n')

  HEIGHT_DECL.lastIndex = 0
  for (const match of source.matchAll(HEIGHT_DECL)) {
    const name = match[1]
    const arithmetic = new RegExp(`(?:\\+=\\s*|[-+*/]\\s*)${name}\\b|\\b${name}\\s*[-+*/]`)
    const useLine = lines.findIndex((l) => arithmetic.test(l) && !l.includes(`const ${name}`))
    if (useLine === -1) continue
    const declLine = lines.findIndex((l) => l.includes(`const ${name}`))
    violations.push({
      file,
      line: declLine + 1,
      rule: 'fixed-height-virtualisation',
      message: `${name} is a fixed pixel height consumed by layout arithmetic on line ${useLine + 1}; styling can change the rendered height without changing the maths, so rows drift or become unreachable. Measure row geometry at runtime instead`,
      snippet: lines[declLine].trim(),
    })
  }
  return violations
}

export function scanSource(filePath, source, exceptions = [], now = new Date()) {
  const file = normalise(filePath)
  if (!isScannable(file)) return []

  const lines = source.split('\n')
  // Matching runs against the comment-stripped text; the reported snippet
  // still shows the original line, since that's what a human needs to see.
  const codeLines = lines.map(stripComments)
  const violations = []

  for (const rule of RULES) {
    if (file === TOKEN_DEFINITION_FILE) continue
    if (!rule.appliesTo(file)) continue

    const hits = []
    codeLines.forEach((codeLine, index) => {
      // matchAll clones the regex internally, so it does not depend on --
      // and does not disturb -- rule.pattern.lastIndex. Each match on the
      // line is its own hit: a line carrying three matches is three
      // occurrences, not one, or a regression added to an already-violating
      // line would never move hits.length.
      for (const _match of codeLine.matchAll(rule.pattern)) {
        hits.push({ line: index + 1, snippet: lines[index].trim() })
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

  violations.push(...scanFixedHeightVirtualisation(file, codeLines.join('\n')))

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

  // Printed whatever the outcome, and never contributing to the exit code.
  for (const warning of expiringExceptions(exceptions, now)) {
    console.warn(`token compliance warning: ${warning.message}`)
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

// process.argv[1] is undefined when this module is imported from `node -e`,
// a REPL, or any other entry point that has no script path -- pathToFileURL
// throws ERR_INVALID_ARG_TYPE on it, so the guard has to check before it
// converts.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) await main()
