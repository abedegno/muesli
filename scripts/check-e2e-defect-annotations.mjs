#!/usr/bin/env node

import { execFile } from 'node:child_process'
import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { promisify } from 'node:util'
import { fileURLToPath, pathToFileURL } from 'node:url'
import ts from 'typescript'

const execFileAsync = promisify(execFile)

async function findSpecFiles(directory) {
  const files = []
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const entryPath = path.join(directory, entry.name)
    if (entry.isDirectory()) files.push(...(await findSpecFiles(entryPath)))
    else if (entry.isFile() && entry.name.endsWith('.spec.ts')) files.push(entryPath)
  }
  return files
}

export async function scanAnnotations(directory) {
  const annotations = []
  for (const file of await findSpecFiles(directory)) {
    const sourceText = await readFile(file, 'utf8')
    const source = ts.createSourceFile(file, sourceText, ts.ScriptTarget.Latest, true)

    function visit(node) {
      if (
        ts.isCallExpression(node) &&
        ts.isPropertyAccessExpression(node.expression) &&
        ts.isIdentifier(node.expression.expression) &&
        node.expression.expression.text === 'test' &&
        (node.expression.name.text === 'fail' || node.expression.name.text === 'skip')
      ) {
        const reasonArgument = node.arguments[1]
        const reason =
          reasonArgument &&
          (ts.isStringLiteral(reasonArgument) || ts.isNoSubstitutionTemplateLiteral(reasonArgument))
            ? reasonArgument.text
            : ''
        annotations.push({
          file,
          line: source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1,
          kind: node.expression.name.text,
          reason,
        })
      }
      ts.forEachChild(node, visit)
    }
    visit(source)
  }
  return annotations
}

export async function checkAnnotations(directory, lookupIssueState) {
  const failures = []
  const warnings = []
  for (const annotation of await scanAnnotations(directory)) {
    const issueMatch = annotation.reason.match(/#(\d+)/)
    if (!issueMatch) {
      failures.push({ ...annotation, issue: null, message: 'no issue reference found' })
      continue
    }

    const issue = Number(issueMatch[1])
    const state = (await lookupIssueState(issue)).toLowerCase()
    if (state === 'closed' && annotation.kind === 'fail') {
      failures.push({ ...annotation, issue, message: 'references a closed issue' })
    } else if (state === 'closed' && annotation.kind === 'skip') {
      warnings.push({ ...annotation, issue, message: 'references a closed issue' })
    }
  }
  return { failures, warnings, exitCode: failures.length > 0 ? 1 : 0 }
}

async function lookupIssueState(issue) {
  const { stdout } = await execFileAsync('gh', ['issue', 'view', String(issue), '--json', 'state'])
  return JSON.parse(stdout).state
}

async function main() {
  const target = path.resolve(process.argv[2] ?? 'e2e/specs')
  const result = await checkAnnotations(target, lookupIssueState)
  for (const warning of result.warnings) {
    console.warn(
      `WARNING: ${warning.file}:${warning.line}: test.${warning.kind} issue #${warning.issue}: ${warning.reason}`
    )
  }
  for (const failure of result.failures) {
    const issue = failure.issue === null ? '' : ` issue #${failure.issue}`
    const reason = failure.reason ? `: ${failure.reason}` : ''
    console.error(
      `ERROR: ${failure.file}:${failure.line}: test.${failure.kind}${issue}: ${failure.message}${reason}`
    )
  }
  process.exitCode = result.exitCode
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main()
}
