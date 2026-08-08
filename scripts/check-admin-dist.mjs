#!/usr/bin/env node

import { readdir, readFile, stat } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const distRoot = path.join(repositoryRoot, 'internal/adminui/dist')
const indexPath = path.join(distRoot, 'index.html')

async function listDist() {
  const entries = []

  async function walk(directory, relativeDirectory = '') {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const relativePath = path.posix.join(relativeDirectory, entry.name)
      entries.push(entry.isDirectory() ? `${relativePath}/` : relativePath)
      if (entry.isDirectory()) await walk(path.join(directory, entry.name), relativePath)
    }
  }

  try {
    await walk(distRoot)
  } catch (error) {
    entries.push(`<unable to list dist: ${error.message}>`)
  }
  return entries.length === 0 ? '<empty>' : entries.join('\n  ')
}

function fail(message, url, resolvedPath, listing) {
  console.error(`Admin embed contract violation: ${message}`)
  console.error(`  URL: ${url ?? '<none>'}`)
  console.error(`  Resolved filesystem path: ${resolvedPath}`)
  console.error(`  Dist contents:\n  ${listing}`)
  process.exitCode = 1
}

const listing = await listDist()
let html
try {
  html = await readFile(indexPath, 'utf8')
} catch (error) {
  fail(`index.html is missing or unreadable (${error.message})`, null, indexPath, listing)
  process.exit()
}

const assetUrls = Array.from(
  html.matchAll(/\b(?:src|href)\s*=\s*(?:"([^"]*)"|'([^']*)')/gi),
  (match) => match[1] ?? match[2]
).filter((url) => !/^(?:https?:)?\/\//i.test(url))

let jsCount = 0
let cssCount = 0

for (const assetUrl of assetUrls) {
  const urlPath = assetUrl.split(/[?#]/, 1)[0]
  let decodedPath
  try {
    decodedPath = decodeURIComponent(urlPath)
  } catch (error) {
    fail(`asset URL cannot be decoded (${error.message})`, assetUrl, distRoot, listing)
    continue
  }

  const segments = decodedPath.split('/')
  const handlerPath = decodedPath.replace(/^\/admin(?:\/|$)/, '').replace(/^\//, '')
  const resolvedPath = path.resolve(distRoot, handlerPath)

  if (!assetUrl.startsWith('/admin/')) {
    fail('local asset URL must start with /admin/', assetUrl, resolvedPath, listing)
  }
  if (segments.includes('..')) {
    fail('asset URL contains a .. path segment', assetUrl, resolvedPath, listing)
  }

  try {
    if (!(await stat(resolvedPath)).isFile()) throw new Error('path is not a file')
  } catch (error) {
    fail(`referenced asset does not exist (${error.message})`, assetUrl, resolvedPath, listing)
  }

  if (/\.js$/i.test(decodedPath)) jsCount += 1
  if (/\.css$/i.test(decodedPath)) cssCount += 1
}

if (jsCount === 0) fail('index.html references no JavaScript asset', null, indexPath, listing)
if (cssCount === 0) fail('index.html references no CSS asset', null, indexPath, listing)

if (process.exitCode) process.exit()
console.log(
  `Admin embed contract passed: ${assetUrls.length} local assets (${jsCount} JS, ${cssCount} CSS) resolve under ${distRoot}`
)
