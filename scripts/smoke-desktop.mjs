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
// Usage:  node scripts/smoke-desktop.mjs <path-to-.app-or-binary> [--port 9444] [--journey]
// Exit:   0 = renderer mounted cleanly / journey passed, 1 = blank/failed (with diagnostics)
//
// Deliberately dependency-free (uses global fetch + WebSocket). On Node 20 run
// it with --experimental-websocket; Node >=22 has WebSocket unflagged.

import { execFileSync, spawn } from 'node:child_process'
import { mkdtempSync, existsSync, readdirSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const args = process.argv.slice(2)
let appArg = null
let journey = false
let PORT = 9444
const launchArgs = []
for (let i = 0; i < args.length; i += 1) {
  const arg = args[i]
  if (arg === '--journey') {
    journey = true
    continue
  }
  if (arg === '--port') {
    PORT = Number(args[i + 1])
    i += 1
    continue
  }
  if (!arg.startsWith('--') && appArg === null) {
    appArg = arg
    continue
  }
  if (arg === appArg) continue
  launchArgs.push(arg)
}
const TARGET_TIMEOUT_MS = 90_000 // app boot + first paint
const MOUNT_TIMEOUT_MS = 60_000 // renderer mount after the target appears
const JOURNEY_STEP_TIMEOUT_MS = 25_000

if (!appArg) {
  console.error(
    'usage: node scripts/smoke-desktop.mjs <path-to-.app-or-binary> [--port N] [--journey]'
  )
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

function getJourneyStateExpression() {
  return `(() => {
    const text = document.body?.innerText ?? ''
    const hasStartupPanel = text.includes('Starting Muesli') || text.includes('Starting...')
    const hasStartupError = text.includes('Startup failed') || text.includes('Muesli could not start')
    const hasDegradedBanner = text.includes('Install Ollama to enable summaries & search.')
    const headings = Array.from(document.querySelectorAll('h2')).map((el) => (el.textContent ?? '').trim())
    const hasSettingsHeading = headings.includes('Server') || headings.includes('Appearance')
    const hasNoNotesYet = text.includes('No notes yet')
    return {
      hasStartupPanel,
      hasStartupError,
      hasDegradedBanner,
      hasSettingsHeading,
      hasNoNotesYet,
    }
  })()`
}

const userDataDir = mkdtempSync(join(tmpdir(), 'muesli-smoke-'))
if (journey) {
  writeFileSync(
    join(userDataDir, 'local-session.json'),
    JSON.stringify({ encrypted: false, manualServer: false, onboarded: true })
  )
}
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
  const appBundlePath = appArg.endsWith('.app') ? appArg : null
  let currentTarget = await findPageTarget(Date.now() + TARGET_TIMEOUT_MS)
  if (!currentTarget) fail(exitInfo || `no page target on :${PORT} within ${TARGET_TIMEOUT_MS}ms`)

  let send = null
  let exceptions = []
  let consoleErrors = []

  async function connectToTarget(target) {
    console.log(`[smoke] devtools target: ${target.title || '(untitled)'}`)
    const ws = new WebSocket(target.webSocketDebuggerUrl)
    await new Promise((resolve, reject) => {
      ws.addEventListener('open', resolve, { once: true })
      ws.addEventListener('error', () => reject(new Error('devtools websocket error')), {
        once: true,
      })
    })

    const session = cdp(ws)
    send = session.send
    exceptions = session.exceptions
    consoleErrors = session.consoleErrors
    await send('Runtime.enable')
  }

  await connectToTarget(currentTarget)

  const failOnExceptions = (reason) => {
    if (exceptions.length) {
      fail(reason, { exceptions, consoleErrors })
    }
  }

  async function waitForRootMount(failMessage) {
    const deadline = Date.now() + MOUNT_TIMEOUT_MS
    let count = -1
    while (Date.now() < deadline) {
      const { result } = await send('Runtime.evaluate', {
        expression: `(() => { const r = document.getElementById('root'); return r ? r.children.length : -1 })()`,
        returnByValue: true,
      })
      count = result?.value ?? -1
      if (count > 0) return count
      if (exitInfo) fail(exitInfo, { exceptions, consoleErrors })
      await sleep(500)
    }

    fail(failMessage, { exceptions, consoleErrors, childCount: count })
  }

  // Poll until the React root has children. A blank window is exactly
  // "root exists but never gets any".
  let childCount = await waitForRootMount(
    'renderer never mounted — #root child count never became positive (blank UI)'
  )
  if (exceptions.length) {
    fail(`renderer mounted but threw ${exceptions.length} uncaught exception(s)`, {
      exceptions,
      consoleErrors,
    })
  }

  const evaluateState = async () => {
    const { result } = await send('Runtime.evaluate', {
      expression: getJourneyStateExpression(),
      returnByValue: true,
    })
    return result?.value ?? {}
  }

  async function waitForJourneyState(deadline, predicate, failMessage) {
    let state = null
    while (Date.now() < deadline) {
      failOnExceptions(failMessage)
      state = await evaluateState()
      if (predicate(state)) return state
      await sleep(500)
    }
    fail(failMessage, { exceptions, consoleErrors, state })
  }

  async function clickText(text, failMessage) {
    const deadline = Date.now() + JOURNEY_STEP_TIMEOUT_MS
    let clicked = false
    while (Date.now() < deadline) {
      failOnExceptions(failMessage)
      const { result } = await send('Runtime.evaluate', {
        expression: `(() => {
          const target = Array.from(document.querySelectorAll('a,button')).find(
            (el) => (el.textContent ?? '').replace(/\\s+/g, ' ').trim() === ${JSON.stringify(text)}
          )
          if (!target) return false
          target.click()
          return true
        })()`,
        returnByValue: true,
      })
      clicked = result?.value === true
      if (clicked) return true
      await sleep(500)
    }
    fail(failMessage, { exceptions, consoleErrors, clicked })
  }

  async function enableKeepRunningInBackground() {
    const deadline = Date.now() + JOURNEY_STEP_TIMEOUT_MS
    while (Date.now() < deadline) {
      failOnExceptions('journey failed: could not enable keep running in the menu bar')
      const { result } = await send('Runtime.evaluate', {
        expression: `(() => {
          const checkbox = document.getElementById('keep-running-in-background')
          if (!checkbox) return false
          if (!checkbox.checked) checkbox.click()
          return checkbox.checked
        })()`,
        returnByValue: true,
      })
      if (result?.value === true) return true
      await sleep(500)
    }
    fail('journey failed: could not enable keep running in the menu bar', {
      exceptions,
      consoleErrors,
    })
  }

  async function reopenAppOnDarwin() {
    if (!appBundlePath) return false

    const closed = await send('Target.closeTarget', { targetId: currentTarget.id })
    if (!closed?.success) {
      fail('journey failed: could not close the current window before reopening', {
        exceptions,
        consoleErrors,
      })
    }

    try {
      execFileSync('open', ['-a', appBundlePath], { stdio: 'pipe' })
    } catch (err) {
      fail(
        `journey failed: could not relaunch the app via activate (${err instanceof Error ? err.message : String(err)})`,
        { exceptions, consoleErrors }
      )
    }

    const reopenDeadline = Date.now() + TARGET_TIMEOUT_MS
    while (Date.now() < reopenDeadline) {
      const reopenedTarget = await findPageTarget(Date.now() + 2000)
      if (reopenedTarget && reopenedTarget.id !== currentTarget.id) {
        currentTarget = reopenedTarget
        await connectToTarget(currentTarget)
        childCount = await waitForRootMount(
          'journey failed: reopened window never mounted a renderer'
        )
        return true
      }
      await sleep(500)
    }

    fail('journey failed: reopened window never appeared after closing the current one', {
      exceptions,
      consoleErrors,
    })
  }

  if (journey) {
    const startupState = await waitForJourneyState(
      Date.now() + JOURNEY_STEP_TIMEOUT_MS,
      (state) => state && !state.hasStartupPanel && !state.hasStartupError,
      'journey failed: app never left startup screen'
    )
    if (startupState?.hasDegradedBanner) {
      console.log('[smoke] note: app is ready but degraded (no Ollama detected)')
    }
    failOnExceptions('journey failed after startup gate')

    await clickText('Settings', 'journey failed: could not find the Settings nav entry')

    await waitForJourneyState(
      Date.now() + JOURNEY_STEP_TIMEOUT_MS,
      (state) => !!state?.hasSettingsHeading,
      'journey failed: Settings screen never rendered a Server or Appearance heading'
    )
    failOnExceptions('journey failed after opening Settings')

    await enableKeepRunningInBackground()

    await clickText('All notes', 'journey failed: could not find the All notes nav entry')

    await waitForJourneyState(
      Date.now() + JOURNEY_STEP_TIMEOUT_MS,
      (state) => !!state?.hasNoNotesYet,
      'journey failed: notes list never rendered the "No notes yet" empty state'
    )
    failOnExceptions('journey failed after returning to All notes')

    if (process.platform === 'darwin') {
      await reopenAppOnDarwin()

      const reopenedStartupState = await waitForJourneyState(
        Date.now() + JOURNEY_STEP_TIMEOUT_MS,
        (state) => state && !state.hasStartupPanel && !state.hasStartupError,
        'journey failed: reopened window never left startup screen'
      )
      if (reopenedStartupState?.hasDegradedBanner) {
        console.log('[smoke] note: reopened app is ready but degraded (no Ollama detected)')
      }
      failOnExceptions('journey failed after reopening the window')
    }
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
