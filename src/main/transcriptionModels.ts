import { createHash } from 'node:crypto'
import { createReadStream, existsSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { mkdir, open, rename, stat, statfs, unlink } from 'node:fs/promises'
import { join } from 'node:path'
import {
  TRANSCRIPTION_MODELS,
  TRANSCRIPTION_LANGUAGES,
  type TranscriptionModel,
  type TranscriptionModelState,
} from '../shared/transcriptionModels'

interface Options {
  directory: string
  bundledDirectory?: string
  fetch?: typeof fetch
  isHosted: () => boolean
  isRecording: () => boolean
  restart: () => void
  catalog?: readonly TranscriptionModel[]
}
export class TranscriptionModelManager {
  private readonly catalog: readonly TranscriptionModel[]
  private download: TranscriptionModelState['download'] = null
  private controller: AbortController | null = null
  constructor(private readonly options: Options) {
    this.catalog = options.catalog ?? TRANSCRIPTION_MODELS
  }
  private preferences() {
    try {
      const saved = JSON.parse(
        readFileSync(join(this.options.directory, 'selection.json'), 'utf8')
      ) as { model?: string; language?: string }
      if (
        this.catalog.some((model) => model.id === saved.model) &&
        TRANSCRIPTION_LANGUAGES.some((language) => language.id === saved.language)
      )
        return { model: saved.model!, language: saved.language! }
    } catch {
      /* A missing preference uses the bundled English model. */
    }
    return { model: 'tiny.en', language: 'en' }
  }
  private model(id: string): TranscriptionModel {
    const model = this.catalog.find((model) => model.id === id)
    if (!model) throw new Error('Choose one of the supported transcription models.')
    return model
  }
  private path(id: string) {
    this.model(id)
    return join(this.options.directory, `ggml-${id}.bin`)
  }
  private localOnly() {
    if (this.options.isHosted())
      throw new Error(
        'Your team server manages its transcription models. This setting is for the built-in server.'
      )
  }
  private async verify(model: TranscriptionModel): Promise<void> {
    if (!model.url) return
    if ((await stat(this.path(model.id))).size !== model.bytes)
      throw new Error('This model download is incomplete. Download it again.')
    const digest = createHash('sha256')
    for await (const chunk of createReadStream(this.path(model.id))) digest.update(chunk)
    if (digest.digest('hex') !== model.sha256)
      throw new Error('Model verification failed. Download it again.')
  }
  async state(): Promise<TranscriptionModelState> {
    const selected = this.preferences()
    if (!this.controller && !this.download) {
      for (const model of this.catalog)
        if (model.url && existsSync(this.path(model.id) + '.download')) {
          this.download = {
            id: model.id,
            status: 'failed',
            percent: 0,
            error: 'A model download was interrupted. Download the model again to retry.',
          }
          break
        }
    }
    const models = await Promise.all(
      this.catalog.map(async (model) => ({
        ...model,
        installed:
          !model.url ||
          (await stat(this.path(model.id))
            .then((file) => file.size === model.bytes)
            .catch(() => false)),
      }))
    )
    return {
      models,
      selected: selected.model,
      language: selected.language,
      download: this.download ? { ...this.download } : null,
    }
  }
  async environment(): Promise<NodeJS.ProcessEnv> {
    const selected = this.preferences(),
      model = this.model(selected.model)
    if (!model.url)
      return existsSync(join(this.options.directory, 'selection.json'))
        ? {
            MUESLI_WHISPER_MODEL_DIR: this.options.bundledDirectory,
            MUESLI_WHISPER_MODEL: 'ggml-tiny.en',
            MUESLI_WHISPER_MODEL_URL: '',
            MUESLI_WHISPER_LANGUAGE: 'en',
            MUESLI_WHISPER_LIVE_MODEL_DIR: this.options.bundledDirectory,
            MUESLI_WHISPER_LIVE_MODEL: 'ggml-tiny.en',
            MUESLI_WHISPER_LIVE_MODEL_URL: '',
            MUESLI_WHISPER_LIVE_LANGUAGE: 'en',
          }
        : {}
    try {
      await this.verify(model)
    } catch {
      this.download = {
        id: model.id,
        status: 'failed',
        percent: 0,
        error:
          'The selected model could not be loaded. Using the included English model; download your preferred model again.',
      }
      return {}
    }
    return {
      MUESLI_WHISPER_MODEL_DIR: this.options.directory,
      MUESLI_WHISPER_MODEL: `ggml-${model.id}`,
      MUESLI_WHISPER_MODEL_URL: '',
      MUESLI_WHISPER_LANGUAGE: selected.language,
      MUESLI_WHISPER_LIVE_MODEL_DIR: this.options.directory,
      MUESLI_WHISPER_LIVE_MODEL: `ggml-${model.id}`,
      MUESLI_WHISPER_LIVE_MODEL_URL: '',
      MUESLI_WHISPER_LIVE_LANGUAGE: selected.language,
    }
  }
  async install(id: string): Promise<void> {
    this.localOnly()
    if (this.controller) throw new Error('A model is already downloading. Wait or cancel it first.')
    const model = this.model(id)
    if (!model.url || !model.sha256) return
    const controller = new AbortController()
    this.controller = controller
    this.download = { id, status: 'downloading', percent: 0 }
    const timeout = setTimeout(() => controller.abort(), 30 * 60_000)
    const temp = this.path(id) + '.download'
    try {
      await mkdir(this.options.directory, { recursive: true, mode: 0o700 })
      const free = await statfs(this.options.directory)
      if (free.bavail * free.bsize < model.bytes + 128 * 1024 * 1024)
        throw new Error('Not enough free disk space for this model. Free some space and try again.')
      const response = await (this.options.fetch ?? fetch)(model.url, { signal: controller.signal })
      if (!response.ok || !response.body)
        throw new Error(`Model download failed (HTTP ${response.status}). Try again.`)
      const file = await open(temp, 'w', 0o600),
        digest = createHash('sha256')
      let bytes = 0
      try {
        const reader = response.body.getReader()
        try {
          while (true) {
            const { value, done } = await reader.read()
            if (done) break
            bytes += value.byteLength
            if (bytes > model.bytes) throw new Error('The model download was larger than expected.')
            digest.update(value)
            await file.writeFile(value)
            this.download = {
              id,
              status: 'downloading',
              percent: Math.floor((bytes / model.bytes) * 100),
            }
          }
        } finally {
          await reader.cancel().catch(() => {})
        }
        if (bytes !== model.bytes || digest.digest('hex') !== model.sha256)
          throw new Error(
            'Model verification failed. The incomplete download was removed; try again.'
          )
        await file.sync()
      } finally {
        await file.close()
      }
      await rename(temp, this.path(id))
      this.download = { id, status: 'done', percent: 100 }
    } catch (error) {
      const cancelled = controller.signal.aborted
      controller.abort()
      await unlink(temp).catch(() => {})
      const message = cancelled
        ? 'Download cancelled or timed out. You can retry.'
        : error instanceof Error
          ? error.message
          : 'Could not download this model.'
      this.download = { id, status: 'failed', percent: 0, error: message }
      throw new Error(message)
    } finally {
      clearTimeout(timeout)
      this.controller = null
    }
  }
  cancel() {
    this.controller?.abort()
  }
  async activate(id: string, language: string): Promise<void> {
    this.localOnly()
    if (this.options.isRecording())
      throw new Error('Stop and save your recording before changing models.')
    if (this.controller) throw new Error('Wait for the model download to finish before restarting.')
    const model = this.model(id)
    if (!TRANSCRIPTION_LANGUAGES.some((item) => item.id === language))
      throw new Error('Choose a supported transcription language.')
    if (!model.multilingual && language !== 'en' && language !== 'auto')
      throw new Error(
        'The included model is English-only. Choose a multilingual model for this language.'
      )
    await this.verify(model)
    await mkdir(this.options.directory, { recursive: true, mode: 0o700 })
    const target = join(this.options.directory, 'selection.json'),
      temp = target + '.tmp'
    writeFileSync(
      temp,
      JSON.stringify({ model: id, language: model.multilingual ? language : 'en' }),
      { mode: 0o600 }
    )
    renameSync(temp, target)
    this.options.restart()
  }
}
