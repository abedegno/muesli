# Desktop Onboarding

This guide explains what the desktop app needs on first launch, what happens
when Ollama is missing, and how to point Muesli at a non-default Ollama
instance.

The first-run wizard shows progress through welcome, microphone, transcription, and optional AI
setup, with Back and Continue controls. Microphone permissions and AI setup can
be completed later. Permission request errors appear in the wizard and can be
retried.

The wizard fits the space below the startup banner. Its step content scrolls in
small windows while progress and navigation remain visible.

On macOS, Muesli uses Keychain to encrypt saved credentials. Locally rebuilt apps
may prompt again for access to “muesli-desktop Safe Storage” because their code
signature changes. See [Electron's safeStorage documentation](https://www.electronjs.org/docs/latest/api/safe-storage).
Automated desktop tests use a mock Keychain with disposable profiles; normal app
launches continue to use the system Keychain.

The welcome step distinguishes the built-in local server from a connected team
server. Local Ollama install instructions appear only after the connection mode
is known and a successful readiness check reports Ollama missing. A failed check
offers a retry without assuming the service is missing. Detecting Ollama confirms
the service is reachable; it does not by itself confirm models are installed.

## Embedded mode

In embedded mode, the desktop app runs its own Postgres, pgvector, and Whisper
services locally. See [EMBEDDED-POSTGRES-BUNDLE.md](EMBEDDED-POSTGRES-BUNDLE.md)
for the embedded database bundle and operational details.

Because those services are bundled with the app, recording and transcription do
not require any external services.

## Transcription choices and settings

The transcription step and Settings share the same controls. **Final transcript**
chooses the service that processes a complete recording; **Live transcript**
chooses the service that shows evolving text during recording, or turns live
text off. Local installations include both Whisper services, with live
transcription enabled by default. Ollama provides optional AI, not transcription.

Selections are stored on the server. Restarting refreshes bundled services'
transient endpoints and tokens without replacing a selected alternative or
re-enabling live transcription. In hosted deployments these are server-wide
choices, and the UI explains that scope.

Custom service URLs and tokens are under an Advanced disclosure. Connection
tests identify authentication, HTTP, and reachability failures. Settings now
explains which options are optional, keeps storage/server switching and scheduled
summary integrations under Advanced, and provides keyboard-accessible help on
transcription and AI controls. Scheduled summaries require a configured delivery
integration; they do not imply built-in email delivery.

## Internationalisation

The renderer's typed English source catalogue is in `src/renderer/i18n/en.ts`.
New transcription controls and settings explanations use whole-message keys and
named placeholders. Regional date and number formatting follows the browser/OS
locale, including note date groups and people/company/insight statistics.
Display language and transcription language are separate concerns.

This is an incremental foundation: existing screens still contain English text,
and translated catalogues and an app-language selector are not yet provided.
New controls use wrapping layouts and help supports keyboard focus; desktop
checks also exercise the settings help layout with right-to-left direction.

## Degraded mode

When the server reports `embedded.ollamaDetected: false` from `/readyz`, the
desktop app enters degraded mode. The probe is implemented in
[`internal/embedded/ollama.go`](../internal/embedded/ollama.go) via
`DetectOllama`, which checks `http://127.0.0.1:11434/api/version` with a
1.5s timeout.

Degraded mode only disables AI summaries and semantic search. Recording,
transcription, and manual note-taking continue to work.

## Install Ollama

On macOS, the wizard's **Download Ollama for Mac** button opens the official
disk-image download. Open the installer, drag Ollama to Applications, then open
it. If it is already installed in `/Applications` or `~/Applications`, the wizard
offers **Open Ollama** instead. This follows [Ollama's Mac installation flow](https://docs.ollama.com/macos).

The wizard checks the live Ollama version endpoint automatically, independently
of the server's cached startup result. Checks stop when the step closes, Ollama
is detected, or five minutes elapse. **Re-check Ollama** starts another check
window. Probes have a 1.5-second timeout, and overlapping requests share one probe.

Once Ollama is running, **Restart and set up AI** completes onboarding and
restarts Muesli with the same profile. The existing embedded startup flow attempts
to download the summary and search models and shows download progress. Default
models need around 3 GB of free disk space. A detected Ollama service is not proof
that model downloads have completed successfully; startup download failures are
currently logged in the server log.

Linux retains the official installation command alongside a download link.
Windows offers the official download page. Hosted connections show administrator
guidance and cannot invoke local installation or restart actions.

## Use a remote Ollama

To point Muesli at a non-default or remote Ollama instance, set
`MUESLI_OLLAMA_URL`.

[`internal/embedded/ollama.go`](../internal/embedded/ollama.go) exposes this
through `OllamaBaseURL()`, which falls back to `DefaultOllamaURL`
(`http://127.0.0.1:11434`) when the variable is unset or blank.

## Where this appears

- First run: `SetupWizard` AI step
- Later launches: Settings → AI / Transcription; AI actions explain when a service is unavailable. Missing optional AI does not add a global startup warning.

## Recording review — 18–19 October 2026

The review covers the local desktop flow and the hosted connection separately.
The automated desktop checks use disposable profiles and synthetic microphone
input; they do not establish that macOS has granted permissions on a particular
user's machine.

### Fixed in this pass

- Embedded upload URLs now use the actual embedded listener. Previously a launch
  on a non-default port could hand the recorder a URL at `localhost:8080`.
- Stopping capture marks an owned, unuploaded recording as stopped on the server.
  A delayed stop cannot regress a note that has already entered processing.
  Capture start records its actual start time and clears an earlier end time,
  rather than leaving meeting-duration data absent or stale.
- Live words remain visible after Stop or a stream interruption, until a complete
  final transcript replaces the preview. Final and live transcription remain
  separate choices; Ollama is optional AI.
- Help stays open when keyboard focus scrolls a control into view, supports
  clicks, and dismisses with Escape or an outside press. The optional-AI startup
  warning no longer crowds setup or the meeting screen.
- Connection tests show pending and outcome feedback beside the service being
  checked. Server health probes have a five-second deadline, and the desktop
  request has a ten-second timeout.
- Idle notes no longer open the microphone. Testing it is an explicit action;
  starting a recording reuses an existing microphone grant. Computer audio has a
  separate, optional setup check because macOS treats it as a separate permission.
- Leaving a recording stops and saves that recording. A late permission response
  cannot start capture after leaving the note. Native computer-audio cleanup is
  scoped to its capture, so stale cleanup cannot stop a newer capture.
- Upload and processing activities have separate identities. Terminal upload
  state is updated even if progress notifications are missed.
- Completed audio is saved privately in the desktop profile before upload, with
  explicit retry/discard feedback after failure. A restart test forces a real
  storage failure, restarts the same disposable profile and successfully retries.
  Backups are bounded to ten recordings, 256 MiB each and 512 MiB in total;
  exhaustion refuses a new backup rather than silently evicting unuploaded audio.
- Connecting to a team server or resetting to the built-in server preserves the
  autogenerated local account credentials. Embedded sessions and saved upload
  recovery survive a change of the local listener port.

### Follow-up implementation

The seven review areas are implemented for both local and hosted deployments:

1. **Hosted permissions:** existing installations promote the original account to
   administrator; new accounts are members. All administrative API routes enforce
   the role on every request. The console checks access before mounting, and its
   Account access form changes existing users' roles. The last administrator
   cannot be demoted. Ordinary members can inspect available transcription
   services but cannot change shared selections or secrets.
2. **Interrupted recording recovery:** mixed 16 kHz PCM is journalled privately on
   disk independently of live transcription. Writes sync after about one second
   of audio; a crash can still lose the unsynced tail. Limits are ten captures,
   256 MiB each and 512 MiB total. A visible inbox offers explicit upload, export
   and confirmed discard. Account identity binds recovery across token rotation;
   authenticated legacy backups migrate without exposing another account's data.
   Per-capture leases last 90 seconds and heartbeats renew every 20 seconds.
   Stale cleanup cannot stop a replacement capture. Expired or legacy sessions
   show Check recording rather than claiming an active capture.
3. **Connected-server readiness:** setup and Settings check the selected final,
   live and AI services on the connected server. Semantic search is tested with
   a fixed non-user embedding request. Probes have a five-second deadline and
   share a five-second per-server cache. Optional AI model downloads report
   progress/failure and permit bounded administrator retries without blocking
   recording. No health request starts downloads.
4. **Language and models:** English, French, German and Spanish display catalogues
   cover the desktop and administration controls. The language can change in the
   wizard or Settings and persists separately from regional date/number formatting
   and spoken language. Missing catalogues and placeholder mismatches fail tests.
   Pinned multilingual Whisper Tiny/Base/Small downloads are size- and
   SHA-256-verified, with progress, cancellation and failure recovery. Activation
   is explicit, persists across restarts and is blocked during recording. The
   selector explains size, memory, privacy and separate live/final model copies.
5. **Meeting canvas:** My notes is the default surface, including after Stop.
   The editor stays mounted while the transcript and summary arrive. Live words
   have a bounded, collapsible region and stop following when the user scrolls
   back. Transcript and Summary remain explicit alternatives, preserving source
   material and personal notes during regeneration.
6. **Calendar onboarding:** distinguish unavailable requests, unconfigured OAuth,
   disconnected calendars, first sync, sync errors and genuinely empty schedules.
   Local provider setup and hosted administrator guidance differ. Browser return
   rechecks the connection. Manual recording stays available without a calendar.
7. **Bounded sessions and release validation:** capture stops and saves at two
   hours or a 64 MiB encoded-audio stop threshold (128 MiB hard cap including
   final chunks). Live preview has separate
   bounded queues and can fail without losing the recording. Hosted concurrency
   tests verify shared readiness probes across twenty sessions and twenty live
   WebSocket connections with isolated audio/text under the race detector. CI supports signed,
   notarized macOS validation without publishing, installs from the built DMG and
   runs the installed app tests. Linux AppImages are extracted and tested under
   Xvfb. Release publication remains restricted to desktop version tags.

### Comparable products and design evidence

This is a source/documentation comparison, not a hands-on certification of these
products. The project found for “typewhisperer” was
[TypeWhisper for macOS](https://github.com/TypeWhisper/typewhisper-mac).

| Reference                                                                                                         | Useful evidence                                                                       | Muesli implication                                                                                                                           |
| ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| [TypeWhisper recovery feedback](https://github.com/TypeWhisper/typewhisper-mac/pull/1273)                         | Its recovery fix makes preserved audio and the recovery action visible after failure. | Preserving bytes is insufficient if the user cannot find them. This informed the explicit saved-recording retry feedback added here.         |
| [TypeWhisper engines and language support](https://github.com/TypeWhisper/typewhisper-mac/blob/main/README.md)    | It exposes engine choices and local/cloud capabilities.                               | Describe language, download and privacy tradeoffs in the model selector, while keeping advanced service configuration out of the basic flow. |
| [Granola personal notes](https://docs.granola.ai/help-center/taking-notes/taking-notes-in-granola)                | Rough personal notes are useful input rather than a formatting task.                  | Let the user write immediately; keep setup and recording state understandable without dominating the editor.                                 |
| [Granola enhanced notes](https://docs.granola.ai/help-center/taking-notes/ai-enhanced-notes)                      | Generated notes remain editable and transcript sources can be inspected.              | Keep original notes and source transcript accessible; make regeneration an explicit action.                                                  |
| [Granola transcription troubleshooting](https://docs.granola.ai/help-center/troubleshooting/transcription-issues) | Microphone and system-audio access are separate setup concerns.                       | Show readiness per audio source and explain the relevant permission beside its action.                                                       |
| [Anarlog, formerly Hyprnote](https://github.com/fastrepl/anarlog)                                                 | An open-source comparison for local notes, transcription and bring-your-own AI.       | Evaluate portable note storage and clear local-versus-cloud choices before adding more integrations.                                         |

Muesli retains recordings. Granola's storage/privacy statements must not be
reused as claims about Muesli. Local recovery files use private filesystem
permissions, not an additional application encryption layer; hosted uploads go
to the configured server. These destinations should remain explicit in the UI.

### Earlier-pass verification

- Client: 1,479 tests across 151 files; coverage thresholds passed (87.82% lines,
  84.67% branches, 78.02% functions). Type checking, token checks and formatting
  passed; ESLint has no errors and the existing twelve warnings.
- Go: the full suite passed with a real disposable embedded PostgreSQL database.
  All store/API tests were run again after fixing capture timestamps.
- Packaged macOS app: six desktop scenarios passed, covering small-window setup,
  keyboard/click help and RTL layout, actual Chromium recording and upload,
  navigation during recording, failed-upload recovery after app restart, and
  alternative/live-off transcription choices across restarts. Recording and
  recovery were checked again after the final server build.
- Regression checks were observed failing before their fixes, including wrong
  upload origin, erased live words, delayed audio cleanup, connection switching,
  unbounded live buffering, tooltip scroll dismissal and missing timestamps.
- Audio input in automation is synthetic and transcription is deterministic.
  These checks do not measure speech-recognition quality or grant native macOS
  microphone/computer-audio permissions. Those still require a manual test.

The local test app is ad-hoc signed and signature verification passed. It is not
an independently notarized release. No manual user profile was reset, and no
production app was published as part of this verification.

### Follow-up validation and practical limits

Packaged macOS tests exercise actual Chromium capture with a synthetic microphone,
normal Stop, navigation during capture, failed-upload restart recovery and SIGKILL
before Stop followed by explicit recovery. Typed personal notes survive Stop.
The real pinned multilingual Tiny model was downloaded, checksum-verified and
loaded by the bundled native engine. It recognised the repository's JFK recording
in English and locally synthesised French speech with automatic French detection.
These samples establish that the real path works; they are not an accuracy benchmark.

Regression guards were observed failing with the protected mechanism broken:
member administration, stale capture replacement, cross-connection editor saves,
checksum rejection, two-hour stopping, missing translation entries and readiness
cache coalescing (sixty probes instead of three for twenty requests).

Native microphone/computer-audio permission dialogs still require human device
checks. A timer-accelerated two-hour bound is not a two-hour acoustic endurance
measurement, and shared-probe concurrency is not a twenty-meeting ASR throughput
benchmark. External OAuth providers require the operator's application credentials.
Translations should receive native-speaker review before claiming editorial quality.

Local follow-up checks: 1,532 client tests across 156 files passed, with coverage
86.99% lines, 84.35% branches and 78.14% functions. The administration suite has
118 passing tests. Both TypeScript projects compile; ESLint has no errors and
eight existing warnings. Token compliance, catalogue extraction and whole-repo
formatting pass. The desktop release workflow passes actionlint. The full Go
suite passed against disposable PostgreSQL; new API/store paths and twenty
simultaneous streaming connections also passed the race detector. Fifteen
packaged scenarios were exercised; after fixing dropdown names, the language
restart and recording lifecycle scenarios were rerun successfully.

Hosted deployments must upgrade the server alongside the client to gain the new
role checks, readiness endpoints and capture-lease support. An older server does
not gain those protections from a desktop update alone.
