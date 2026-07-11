# SEC06 — Dependency Advisory Sweep

**Date:** 2026-06-29  
**Scope:** Go server + Electron/Node client  
**Tools:** `govulncheck`, `go list -m -u all`, `npm audit`, `npm outdated`

---

## What Was Applied

### Go side

| Change                                     | Before    | After      | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ------------------------------------------ | --------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `go` directive in go.mod (toolchain floor) | go 1.25.6 | go 1.25.11 | Raises the **minimum required toolchain**, ensuring all builds use a Go release that includes fixes for **10 stdlib security advisories** (GO-2026-4337, GO-2026-4601, GO-2026-4602, GO-2026-4870, GO-2026-4918, GO-2026-4946, GO-2026-4947, GO-2026-4971, GO-2026-5037, GO-2026-5039) covering crypto/tls, crypto/x509, net, net/http, net/textproto, net/url, and os. govulncheck reports **No vulnerabilities found** after raising this floor. |

No direct runtime Go module dependencies (`chi`, `golang-migrate`, `pgx`, `uuid`, `golang.org/x/crypto`) had newer versions available; all are already at their latest releases.

### Node side

| Package                         | Before           | After            | Notes                                     |
| ------------------------------- | ---------------- | ---------------- | ----------------------------------------- |
| `electron`                      | 42.4.0           | 42.5.0           | Patch within `^42` range (devDep)         |
| `@tiptap/pm`                    | ^3.26.1 (3.26.1) | ^3.27.1 (3.27.1) | Minor update                              |
| `@tiptap/react`                 | ^3.26.1 (3.26.1) | ^3.27.1 (3.27.1) | Minor update                              |
| `@tiptap/starter-kit`           | ^3.26.1 (3.26.1) | ^3.27.1 (3.27.1) | Minor update                              |
| `@radix-ui/react-context-menu`  | 2.3.0            | 2.3.1            | Patch, via npm update (package-lock.json) |
| `@radix-ui/react-dialog`        | 1.1.16           | 1.1.17           | Patch, via npm update (package-lock.json) |
| `@radix-ui/react-dropdown-menu` | 2.1.17           | 2.1.18           | Patch, via npm update (package-lock.json) |
| `@radix-ui/react-scroll-area`   | 1.2.11           | 1.2.12           | Patch, via npm update (package-lock.json) |
| `@radix-ui/react-toast`         | 1.2.16           | 1.2.17           | Patch, via npm update (package-lock.json) |
| `@radix-ui/react-toggle-group`  | 1.1.12           | 1.1.13           | Patch, via npm update (package-lock.json) |
| `@radix-ui/react-tooltip`       | 1.2.9            | 1.2.10           | Patch, via npm update (package-lock.json) |
| `lucide-react`                  | 1.18.0           | 1.22.0           | Minor update within ^1.18.0 range         |
| `react-router-dom`              | 7.17.0           | 7.18.0           | Minor update within ^7.17.0 range         |

`npm audit fix` was run but made no changes (all remaining Node advisories require --force / breaking updates).

---

## Deferred Items

### Node — advisory vulnerabilities (all dev dependencies)

| Package          | Current               | Available fix  | Reason deferred                                                                                                                                                              |
| ---------------- | --------------------- | -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `esbuild`        | <=0.24.2 (transitive) | via vite@8.1.0 | Fix requires npm audit fix --force; would install vite@8.1.0 (major, breaking). All affected packages are **devDependencies** only — not bundled in production Electron app. |
| `vite`           | 5.4.21                | 8.1.0 (major)  | Major version — breaking API changes; affects build tool only (devDep).                                                                                                      |
| `electron-vite`  | 2.3.0                 | 5.0.0 (major)  | Major version — breaking; devDep.                                                                                                                                            |
| `vitest`         | 2.1.9                 | 4.1.9 (major)  | Major version — breaking; devDep.                                                                                                                                            |
| `@vitest/mocker` | transitive            | via vitest 4.x | Transitive devDep; resolved by vitest major bump above.                                                                                                                      |
| `vite-node`      | transitive            | via vitest 4.x | Transitive devDep; resolved by vitest major bump above.                                                                                                                      |

### Node — outdated runtime deps (major version — API-breaking)

| Package     | Current | Latest | Reason deferred                                                                                  |
| ----------- | ------- | ------ | ------------------------------------------------------------------------------------------------ |
| `react`     | 18.3.1  | 19.2.7 | Major version — breaking API changes. Requires coordinated upgrade with react-dom and ecosystem. |
| `react-dom` | 18.3.1  | 19.2.7 | Same as react above.                                                                             |

### Node — outdated dev deps (major version — skip per policy)

| Package                | Current  | Latest  | Reason deferred                                    |
| ---------------------- | -------- | ------- | -------------------------------------------------- |
| `@types/node`          | 20.19.43 | 26.0.1  | Major, devDep — requires Node 26 target types.     |
| `@types/react`         | 18.3.31  | 19.2.17 | Major, devDep — must match react major version.    |
| `@types/react-dom`     | 18.3.7   | 19.2.3  | Major, devDep — must match react-dom.              |
| `@vitejs/plugin-react` | 4.7.0    | 6.0.3   | Major, devDep.                                     |
| `electron-vite`        | 2.3.0    | 5.0.0   | Major, devDep — see vuln section above.            |
| `jsdom`                | 24.1.3   | 29.1.1  | Major, devDep.                                     |
| `typescript`           | 5.9.3    | 6.0.3   | Major, devDep — TypeScript 6 has breaking changes. |
| `vite`                 | 5.4.21   | 8.1.0   | Major, devDep + vuln — see vuln section above.     |
| `vitest`               | 2.1.9    | 4.1.9   | Major, devDep + vuln — see vuln section above.     |

### Go — indirect dependency with available update

| Package                      | Current              | Available            | Reason deferred                                                                                                |
| ---------------------------- | -------------------- | -------------------- | -------------------------------------------------------------------------------------------------------------- |
| `github.com/jackc/pgerrcode` | 0.0.0-20220416144525 | 0.0.0-20250907135507 | Indirect dep — managed automatically by go mod tidy when pgx bumps. No action needed until pgx itself updates. |

---

## Verification

```
go build ./...       ✓
go vet ./...         ✓
govulncheck ./...    ✓  No vulnerabilities found
tsc --noEmit         ✓
vitest run           ✓  377 tests passed
```
