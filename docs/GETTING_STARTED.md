# Getting Started

This walkthrough is for a brand-new Muesli user setting up the stack for the
first time.

## 1. Check prerequisites

Before you run the stack, make sure the host meets the same requirements called
out in the README:

- Docker 24 or newer, with Compose v2 (`docker compose`, not legacy
  `docker-compose`)
- Enough RAM for the default CPU setup, with 8 GB recommended
- Enough disk space for the default model downloads and your recordings
- CPU inference works out of the box; GPU acceleration is optional if you want
  faster transcription and summarization

For more detail, see the [Requirements section in the README](../README.md#requirements).

## 2. Start the stack

From the repository root, bring everything up with:

```bash
docker compose up
```

That starts Postgres, Ollama, the Whisper transcriber, the LLM agent, and the
Muesli server.

The first boot is slow. The `ollama-pull` service downloads the default LLM and
Whisper fetches its model on the first transcription. That delay is normal.

## 3. Create your account in the admin UI

Open the admin UI at <http://localhost:8080/admin>.

On first run, create the account there. You do not need to register the default
plugins manually: the transcriber and agent are auto-registered on startup when
their default URLs and tokens are present.

## 4. Connect the desktop client

The desktop app is the Electron client in the repo root. For development, the
usual launcher is:

```bash
npm install
npm run dev
```

If you want to preview the built app instead, run `npm run build` and then
`npm run start`.

In the app, open the Connect screen and enter:

- Server URL: `http://localhost:8080`
- Email: the account you created in `/admin`
- Password: that account's password

On the very first connect, check **First run (create the account)** so the
client performs initial setup before it logs in. After that, leave the box
unchecked and just connect with your credentials.

The client stores its app token locally after a successful connect, so you
normally only need to do this once per machine.

## 5. Record your first meeting

Use **New meeting** in the desktop client, start recording, and stop when you
are done. The note moves through the pipeline in this order:

`recording` -> `uploaded` -> `transcribing` -> `summarizing` -> `ready`

What that means:

- `recording` - audio is still being captured locally
- `uploaded` - the recording has been sent to the server
- `transcribing` - Whisper is turning the audio into a transcript
- `summarizing` - the LLM agent is writing the meeting summary
- `ready` - the note is complete and viewable

## 6. Expect slow model downloads on first boot

The first transcription and summarization can take a while because two model
downloads happen the first time you exercise the pipeline:

- `ollama-pull` downloads `llama3.2:3b`, which is about 2 GB
- Whisper fetches its model on the first transcription

This is expected and not a bug. Once the models are cached, later runs are much
faster.

## 7. Know where your data lives

Muesli persists its data in two places:

- Postgres stores the structured data: notes, transcripts, summaries, jobs, and
  related metadata
- The internal storage blob store keeps uploaded audio

In Docker Compose, those map to the `pgdata` and `audio` volumes. The audio
volume is mounted in the server container at `/data/audio`, and outside Compose
the default storage path is `./data/audio`.

For the backup and restore details, see [docs/BACKUP.md](BACKUP.md).

## 8. Next places to look

- [docs/TROUBLESHOOTING.md](TROUBLESHOOTING.md) if something is slow, stuck, or
  failing
- [docs/CONFIGURATION.md](CONFIGURATION.md) to customize environment variables

