# Streaming Transcript Smoke Checklist

Prereq: the normal stack is up, plus the optional streaming profile:
`docker compose --profile streaming up`.

- [ ] Start a short recording in the desktop client with the streaming plugin
      registered and enabled.
- [ ] Speak for a few seconds and watch the note screen's `Live transcript`
      panel append finalized utterances as they arrive.
- [ ] Stop the recording and confirm the note's `Transcript` view replaces the
      provisional live segments with the final batch transcript.
- [ ] Disable or unregister the streaming plugin and confirm the panel
      degrades to `Live transcript unavailable`.
- [ ] Re-enable the plugin and confirm the live panel returns on the next
      recording.
