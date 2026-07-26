#!/usr/bin/env node
// Packaged-app smoke test: launch the BUILT desktop app and prove the renderer
// actually mounts.
//
// Why this exists: desktop v0.1.10 shipped a renderer that threw on first
// bridge access (a Proxy over `contextBridge`'s read-only, non-configurable
// members violates a JS proxy invariant). React never mounted — every launch
// was a blank window — yet the whole unit suite was green, because tests stub
// `window.muesli` with a plain object whose properties ARE configurable. Only
// the real packaged app reproduces it. This is the gate that catches that
// class of bug: it drives the actual .app over the Chrome DevTools Protocol and
// asserts the UI rendered.
//
// Usage:  node scripts/smoke-desktop.mjs <path-to-.app-or-binary> [--port 9444]
// Exit:   0 = renderer mounted cleanly, 1 = blank/failed (with diagnostics)
//
// Deliberately dependency-free (uses global fetch + WebSocket). On Node 20 run
// it with --experimental-websocket; Node >=22 has WebSocket unflagged.

import { spawn } from 'node:child_process'
import { mkdtempSync, existsSync, readdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const args = process.argv.slice(2)
const appArg = args.find((a) => !a.startsWith('--'))
const portArg = args.indexOf('--port')
const PORT = portArg !== -1 ? Number(args[portArg + 1]) : 9444
const launchArgs = []
for (let i = 0; i < args.length; i += 1) {
  const arg = args[i]
  if (arg === appArg) continue
  if (arg === '--port') {
    i += 1
    continue
  }
  launchArgs.push(arg)
}
const TARGET_TIMEOUT_MS = 90_000 // app boot + first paint
const MOUNT_TIMEOUT_MS = 60_000 // renderer mount after the target appears

if (!appArg) {
  console.error('usage: node scripts/smoke-desktop.mjs <path-to-.app-or-binary> [--port N]')
  process.exit(2)
}

if (typeof WebSocket === 'undefined') {
  console.error(
    'error: no global WebSocket. Re-run with: node --experimental-websocket scripts/smoke-desktop.mjs ...'
  )
  process.exit(2)
}

function resolveBinary(p) {
  if (!p.endsWith('.app')) return p
  const macos = join(p, 'Contents', 'MacOS')
  if (!existsSync(macos)) throw new Error(`no Contents/MacOS in ${p}`)
  const entries = readdirSync(macos)
  if (entries.length === 0) throw new Error(`no executable in ${macos}`)
  return join(macos, entries[0])
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function findPageTarget(deadline) {
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/json/list`)
      const targets = await res.json()
      const page = targets.find((t) => t.type === 'page' && t.webSocketDebuggerUrl)
      if (page) return page
    } catch {
      // devtools endpoint not up yet
    }
    await sleep(500)
  }
  return null
}

function cdp(ws) {
  let nextId = 1
  const pending = new Map()
  const exceptions = []
  const consoleErrors = []

  ws.addEventListener('message', (ev) => {
    let msg
    try {
      msg = JSON.parse(ev.data)
    } catch {
      return
    }
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id)
      pending.delete(msg.id)
      msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result)
      return
    }
    if (msg.method === 'Runtime.exceptionThrown') {
      const d = msg.params?.exceptionDetails
      exceptions.push(d?.exception?.description || d?.text || JSON.stringify(d))
    }
    if (msg.method === 'Runtime.consoleAPICalled' && msg.params?.type === 'error') {
      consoleErrors.push((msg.params.args || []).map((a) => a.description ?? a.value).join(' '))
    }
  })

  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const id = nextId++
      pending.set(id, { resolve, reject })
      ws.send(JSON.stringify({ id, method, params }))
    })

  return { send, exceptions, consoleErrors }
}

const userDataDir = mkdtempSync(join(tmpdir(), 'muesli-smoke-'))
const binary = resolveBinary(appArg)
console.log(`[smoke] launching ${binary}`)
console.log(`[smoke] clean user-data-dir: ${userDataDir}`)

// Bind the embedded server off the default port. Without this the smoke run
// collides with any already-running instance (both try 127.0.0.1:8080, the
// second aborts with "address already in use" and the app exits) — which would
// make this test fail for a reason that has nothing to do with the build.
const serverAddr = `127.0.0.1:${PORT + 1000}`
console.log(`[smoke] embedded server addr: ${serverAddr}`)

const child = spawn(
  binary,
  [`--remote-debugging-port=${PORT}`, `--user-data-dir=${userDataDir}`, ...launchArgs],
  {
    stdio: ['ignore', 'pipe', 'pipe'],
    detached: true,
    env: { ...process.env, MUESLI_ADDR: serverAddr },
  }
)
const appOut = []
child.stdout.on('data', (d) => appOut.push(String(d)))
child.stderr.on('data', (d) => appOut.push(String(d)))

let exitInfo = null
child.on('exit', (code, signal) => {
  exitInfo = `app exited early (code=${code} signal=${signal})`
})

function shutdown() {
  try {
    process.kill(-child.pid, 'SIGKILL') // kill the group: app + embedded server
  } catch {
    try {
      child.kill('SIGKILL')
    } catch {
      /* already gone */
    }
  }
}

function fail(reason, extra = {}) {
  console.error(`\n[smoke] FAIL: ${reason}`)
  for (const [k, v] of Object.entries(extra)) {
    if (v && (!Array.isArray(v) || v.length))
      console.error(`[smoke] ${k}: ${JSON.stringify(v, null, 2)}`)
  }
  const tail = appOut.join('').split('\n').slice(-40).join('\n')
  if (tail.trim()) console.error(`[smoke] app output (tail):\n${tail}`)
  shutdown()
  process.exit(1)
}

try {
  const target = await findPageTarget(Date.now() + TARGET_TIMEOUT_MS)
  if (!target) fail(exitInfo || `no page target on :${PORT} within ${TARGET_TIMEOUT_MS}ms`)
  console.log(`[smoke] devtools target: ${target.title || '(untitled)'}`)

  const ws = new WebSocket(target.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    ws.addEventListener('open', resolve, { once: true })
    ws.addEventListener('error', () => reject(new Error('devtools websocket error')), {
      once: true,
    })
  })

  const { send, exceptions, consoleErrors } = cdp(ws)
  await send('Runtime.enable')

  // Poll until the React root has children. A blank window is exactly
  // "root exists but never gets any".
  const deadline = Date.now() + MOUNT_TIMEOUT_MS
  let childCount = -1
  while (Date.now() < deadline) {
    const { result } = await send('Runtime.evaluate', {
      expression: `(() => { const r = document.getElementById('root'); return r ? r.children.length : -1 })()`,
      returnByValue: true,
    })
    childCount = result?.value ?? -1
    if (childCount > 0) break
    if (exitInfo) fail(exitInfo, { exceptions, consoleErrors })
    await sleep(500)
  }

  if (childCount <= 0) {
    fail(`renderer never mounted — #root child count = ${childCount} (blank UI)`, {
      exceptions,
      consoleErrors,
    })
  }
  if (exceptions.length) {
    fail(`renderer mounted but threw ${exceptions.length} uncaught exception(s)`, {
      exceptions,
      consoleErrors,
    })
  }

  const { result: title } = await send('Runtime.evaluate', {
    expression: 'document.title',
    returnByValue: true,
  })
  console.log(
    `[smoke] PASS — #root has ${childCount} child element(s); title="${title?.value ?? ''}"`
  )
  if (consoleErrors.length) {
    console.log(`[smoke] note: ${consoleErrors.length} console error(s) (non-fatal):`)
    for (const e of consoleErrors.slice(0, 5)) console.log(`[smoke]   ${e}`)
  }
  shutdown()
  process.exit(0)
} catch (err) {
  fail(err instanceof Error ? err.message : String(err))
}
