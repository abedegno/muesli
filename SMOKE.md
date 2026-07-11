# Muesli Desktop — Manual Smoke Checklist

Prereq: a running Muesli server (`docker compose up` from the server repo) reachable
at a URL, with the transcriber + agent plugins configured (Plan 1b/2). Run the app
with `npm run dev`.

## 1. Connect / first run

- [ ] `npm run dev` (electron-vite dev) boots: Electron main process loads without
      an ESM/module-format error, preload bridge attaches, window opens.
- [ ] Launch app → Connect screen appears (no saved config).
- [ ] Enter server URL. For a brand-new server, tick "first-time setup", enter
      email + password → Connect → lands in the sidebar shell on "New meeting".
- [ ] Quit and relaunch → it skips Connect and opens the sidebar shell (token persisted).
- [ ] Inspect `userData/muesli-credentials.json` → token is **not** human-readable
      (encrypted) and `"encrypted": true` on a machine with a keychain.

## 2. Capture a meeting

- [ ] Click "New meeting" → the note editor opens focused. Type a title and some
      live Markdown notes.
- [ ] Click "Record". Grant mic + screen/system-audio permission when prompted; the
      capture timer starts running.
- [ ] Play some audio (e.g. a video) and speak into the mic for ~20s.
- [ ] If system audio could not be captured, the mic-only warning is shown
      (expected on macOS without a loopback driver — documented limitation).
- [ ] Click "Stop" → the Processing banner appears and the upload phase advances
      `requesting-url → uploading-audio → confirming-upload → done`.

## 3. Processing + view

- [ ] The Processing banner cycles through "Uploaded — queued…", "Transcribing…",
      and "Summarizing…" then clears when the note reaches `ready`. NOTE: v1 records
      a single mixed mic+system track, so segments carry a single `source` value
      (e.g. `"mixed"`) — there is **no** per-source mic/system attribution.
- [ ] When `ready`, the Enhanced tab shows the summary (Markdown), My notes shows
      what you typed, and Transcript shows the segments.
- [ ] In Enhanced, click a summary citation chip (e.g. `[1]`) and verify it
      switches to Transcript, scrolls to the cited segment, and applies the
      highlight to that line.
- [ ] Open Transcript, confirm the audio player appears for notes with audio, then
      click a transcript line and verify playback seeks to that segment and the
      active line highlight follows the playhead.
- [ ] Open a note -> overflow menu -> Export -> Markdown / Plain Text / Word -> confirm the save dialog defaults to the server-suggested filename and the saved file matches the server export for the chosen format.

## 4. Notes list

- [ ] The meeting appears in the sidebar list with a `ready` badge and a snippet.
- [ ] Open an older note → it renders without re-processing.
- [ ] Rename a note (title blur persists) → the sidebar entry updates.

## 5. Retranscribe / Enhance

- [ ] Open a `ready` or `failed` note, then use the overflow menu → `Enhance…` /
      `Re-transcribe` to open the dialog.
- [ ] The dialog shows both `Model override` and `Language override` inputs.
- [ ] Leave both inputs blank and submit → the note re-transcribes with its
      existing settings, with no validation error.
- [ ] Fill in just a model, just a language, or both → the values are passed
      through to the retranscribe request.
- [ ] After submit, the note transitions back into the processing pipeline
      (`transcribing` and then the usual downstream states) and eventually
      returns to `ready` when processing finishes.

## 6. Settings + disconnect

- [ ] Toggle dark mode in Settings → contrast holds across all screens.
- [ ] Click "Disconnect" → returns to Connect; relaunch stays on Connect
      (credentials cleared).

## Known v1 limitations (see backlog.md)

- System-audio capture: reliable on Windows, partial on macOS (screen-share audio
  only / needs loopback driver), PipeWire-dependent on Linux. Native per-OS capture
  (ScreenCaptureKit/Core Audio, WASAPI, PipeWire) is deferred.
- **Single mixed track:** mic + system are mixed into ONE Opus track, so the
  transcript cannot be split into mic-vs-system segments — `source` is a single
  value (e.g. `"mixed"`). True per-source attribution requires recording two
  separate tracks (mic and system) and transcribing each independently — deferred
  to backlog ("Per-source transcript separation").
- Single account; no resumable/multipart upload; polling (no push).

## UX03 Activity feed for background progress

The activity feed is a floating panel (bottom-right, above the toast viewport)
that surfaces upload and processing progress globally — it stays visible even
when the user navigates away from the note.

- [ ] **Start a recording** → click Stop.
  - The activity feed panel appears in the bottom-right corner (above any toasts).
  - An upload row appears: `<note title> — Preparing upload…`
  - The label transitions through the phases:
    - `Preparing upload…` → `Uploading audio…` → `Confirming…` → `Upload complete`
  - The ✓ indicator appears on the row briefly after `Upload complete`, then the
    row disappears automatically after ~2 seconds.

- [ ] **Processing transitions** (while the note is in the server pipeline):
  - After upload completes, a processing row appears: `<note title> — Uploaded`
  - The label transitions: `Uploaded` → `Transcribing` → `Summarizing` → `Ready`
  - On `Ready`, the ✓ indicator shows briefly, then the row auto-removes after ~2 s.

- [ ] **Navigate away mid-processing**: open another note while the feed is active.
  - The feed rows remain visible (global panel, not tied to the note screen).

- [ ] **× dismiss button**: click × on any feed row → the row disappears immediately
      without waiting for the 2-second auto-remove.

- [ ] **Feed hidden when idle**: once all items have been dismissed or auto-removed,
      the panel disappears entirely (returns `null`, no empty container left in the DOM).

- [ ] **Accessibility**: `aria-live="polite"` on the feed container means screen
      readers announce new feed entries without interrupting the current focus.

## UX04 — Audio device selection + gain (manual cross-platform)

- Launch the app; open a new note; expand recording controls.
- **Device picker**: if multiple input devices are available, switch to a non-default mic, click Record, verify audio is captured from the selected device (waveform or recording result).
- **Gain**: move the slider to 50% and 200%; verify the captured audio is quieter/louder accordingly.
- **Persistence**: stop recording, reload the app, confirm the previously selected device and gain are restored.
- **Permission denied (macOS)**: revoke mic permission in System Settings → Privacy & Security → Microphone; click Record; verify the denial message and macOS-specific recovery hint appear; grant permission; click Retry; verify normal operation resumes.
- **Permission denied (generic)**: verify the fallback hint text appears on non-macOS.
- **Single device**: when only one audio input exists, the picker should still render (showing the single device) but selecting it has no effect on behavior.
- **Unplugged/invalid persisted device**: with a device ID saved in prefs, unplug or remove that mic, then restart and click Record; verify the "unavailable microphone" message appears and the picker is visible (so a new mic can be selected before clicking Retry).

## CALUI02 — "Coming up" calendar view

Prereq: a calendar source configured on the server (CALUI01) with at least a
few upcoming events across different days, some with attendees and/or a
conferencing URL.

- [ ] **Sidebar entry**: click "Coming up" (calendar icon) near the bottom of
      the sidebar, alongside Chat/Trash/Settings → navigates to `/coming-up`.
- [ ] **Grouped by day**: events render under day headings (Today, Tomorrow,
      then weekday/date) for the next 7 days, in chronological order.
- [ ] **Event row content**: each event shows its title, a human-readable
      start–end time, and an attendee count.
- [ ] **Conferencing indicator**: an event with a non-empty `conferencing_url`
      shows a small video-call badge/icon; an event without one does not.
- [ ] **Empty day**: a day in the 7-day window with no events renders "No
      events" (no dangling empty list container).
- [ ] **No calendar / no events**: with no calendar source configured (or a
      calendar with nothing in the next 7 days), the screen shows a friendly
      empty state ("No upcoming events") — no crash, no error boundary.

## CALLNK02 — Note <-> calendar-event link (client)

Prereq: same calendar source setup as CALUI02, plus at least one existing note.

- [ ] **Open a note**: in the note header, next to the overflow ("...") menu,
      a "Link to event" control is visible when the note has no linked event.
- [ ] **Pick an event**: click "Link to event" → a dropdown lists upcoming
      events (title + start–end time, next 7 days); choose one.
- [ ] **Linked state**: the control now shows the linked event's title and
      start–end time (same time format as the "Coming up" view) plus an
      "Unlink" action, instead of the picker.
- [ ] **Persistence**: reload the note (or relaunch) → the link is still shown
      (backed by the note's `event_id`, GET `/api/notes/{id}/full`).
- [ ] **Unlink**: click "Unlink" → the control reverts to "Link to event".
- [ ] **Empty picker state**: on a note with no upcoming events available (or
      no calendar source configured), opening "Link to event" shows "No
      upcoming events" — no crash, no broken UI.
- [ ] **No link state**: a note with no `event_id` shows the "Link to event"
      affordance by default — no crash, no broken UI.

## CALDET02 — Meeting detection loop -> record prompt / auto-record

Prereq: same calendar source setup as CALUI02/CALLNK02, with at least one
current in-progress meeting and a way to toggle `autoRecordDetectedMeetings`
in localStorage for the client.

- [ ] **Prompt on detection**: with `autoRecordDetectedMeetings` off, a current
      meeting surfaces a non-blocking "Record <title>?" banner within about a
      minute, or immediately on refocus.
- [ ] **Accept**: click `Accept` on the banner → capture starts, the note is
      linked to the calendar event, and the linked event is visible in the
      existing note header link UI.
- [ ] **Dismiss**: click `Dismiss` on the banner → that occurrence stays
      suppressed; refocusing the app does not re-show the same meeting.
- [ ] **Auto-record**: with `autoRecordDetectedMeetings` on, the same meeting
      auto-starts capture and links the note without showing the prompt.
- [ ] **Graceful fallback**: no calendar source, no matching events, or an
      already-recording note all result in no visible prompt and no crash.
