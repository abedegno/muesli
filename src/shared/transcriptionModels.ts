export interface TranscriptionModel {
  id: string
  name: string
  bytes: number
  memoryMB: number
  multilingual: boolean
  url?: string
  sha256?: string
}
// Immutable upstream model revision and LFS SHA-256 values, verified against
// https://huggingface.co/api/models/ggerganov/whisper.cpp/tree/5359861c739e955e79d9a303bcbc70fb988958b1
// Memory estimates: https://github.com/ggml-org/whisper.cpp#memory-usage
const revision = '5359861c739e955e79d9a303bcbc70fb988958b1'
const url = (id: string) =>
  `https://huggingface.co/ggerganov/whisper.cpp/resolve/${revision}/ggml-${id}.bin`
export const TRANSCRIPTION_MODELS: readonly TranscriptionModel[] = [
  { id: 'tiny.en', name: 'Whisper Tiny English', bytes: 0, memoryMB: 273, multilingual: false },
  {
    id: 'tiny',
    name: 'Whisper Tiny',
    bytes: 77691713,
    memoryMB: 273,
    multilingual: true,
    url: url('tiny'),
    sha256: 'be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21',
  },
  {
    id: 'base',
    name: 'Whisper Base',
    bytes: 147951465,
    memoryMB: 388,
    multilingual: true,
    url: url('base'),
    sha256: '60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe',
  },
  {
    id: 'small',
    name: 'Whisper Small',
    bytes: 487601967,
    memoryMB: 852,
    multilingual: true,
    url: url('small'),
    sha256: '1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b',
  },
]
export const TRANSCRIPTION_LANGUAGES: ReadonlyArray<{ id: string; name: string }> = [
  { id: 'auto', name: 'Detect automatically' },
  { id: 'en', name: 'English' },
  { id: 'fr', name: 'Français' },
  { id: 'de', name: 'Deutsch' },
  { id: 'es', name: 'Español' },
  { id: 'it', name: 'Italiano' },
  { id: 'pt', name: 'Português' },
  { id: 'nl', name: 'Nederlands' },
  { id: 'ja', name: '日本語' },
  { id: 'zh', name: '中文' },
  { id: 'ko', name: '한국어' },
  { id: 'uk', name: 'Українська' },
  { id: 'ar', name: 'العربية' },
  { id: 'hi', name: 'हिन्दी' },
]
export interface TranscriptionModelState {
  models: Array<TranscriptionModel & { installed: boolean }>
  selected: string
  language: string
  download: {
    id: string
    percent: number
    status: 'downloading' | 'failed' | 'done'
    error?: string
  } | null
}
