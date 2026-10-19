import { uiText } from '@/i18n'
import { TranscriptionModels } from './TranscriptionModels'
import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { HelpTip } from '@/components/ui/HelpTip'
import { t } from '@/i18n'
import type { MuesliBridge } from '../../shared/ipc'
import type { Plugin, TranscriberKind } from '../../shared/types'

export type TranscriptionBridge = Pick<
  MuesliBridge,
  | 'listPlugins'
  | 'getManualServer'
  | 'selectTranscriptionPlugin'
  | 'saveTranscriptionService'
  | 'checkPluginHealth'
> &
  Partial<
    Pick<
      MuesliBridge,
      | 'getCapabilities'
      | 'getTranscriptionModels'
      | 'installTranscriptionModel'
      | 'cancelTranscriptionModelDownload'
      | 'activateTranscriptionModel'
    >
  >

function name(plugin: Plugin, hosted: boolean | null): string {
  if (plugin.name === 'Embedded whisper.cpp transcriber')
    return t(hosted ? 'transcription.bundledServer' : 'transcription.bundled')
  if (plugin.name === 'Embedded whisper.cpp streaming transcriber')
    return t(hosted ? 'transcription.bundledLiveServer' : 'transcription.bundledLive')
  return plugin.name
}

export function TranscriptionPreferences({
  bridge,
  onBusyChange,
}: {
  bridge: TranscriptionBridge
  onBusyChange?: (busy: boolean) => void
}) {
  const [plugins, setPlugins] = useState<Plugin[]>([])
  const [hosted, setHosted] = useState<boolean | null>(null)
  const [canManage, setCanManage] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [checking, setChecking] = useState<string | null>(null)
  const [healthResults, setHealthResults] = useState<
    Partial<Record<TranscriberKind, { id: string; message: string }>>
  >({})
  const busy = saving || checking !== null
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [kind, setKind] = useState<TranscriberKind>('transcriber')
  const [url, setUrl] = useState('')
  const [token, setToken] = useState('')
  const generation = useRef(0)

  async function load() {
    const request = ++generation.current
    setLoading(true)
    setError(null)
    try {
      const [list, manual] = await Promise.all([bridge.listPlugins(), bridge.getManualServer()])
      const permissions = manual ? await bridge.getCapabilities?.() : null
      if (request === generation.current) {
        setCanManage(manual === false || permissions?.isAdmin === true)
        setPlugins(list)
        setHosted(manual)
      }
      return request === generation.current
    } catch {
      if (request === generation.current) setError(t('transcription.loadError'))
      return false
    } finally {
      if (request === generation.current) setLoading(false)
    }
  }

  useEffect(() => {
    void load()
    return () => {
      // This is a request-generation counter, not a DOM ref; invalidate the latest request.
      // eslint-disable-next-line react-hooks/exhaustive-deps
      generation.current++
    }
    // The bridge is a stable dependency; load is intentionally refreshed when
    // a new connected session supplies another bridge.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bridge])

  async function testConnection(purpose: TranscriberKind, id: string) {
    setChecking(id)
    onBusyChange?.(true)
    setHealthResults((current) => ({
      ...current,
      [purpose]: { id, message: t('transcription.checking') },
    }))
    setMessage(null)
    setError(null)
    try {
      const health = await bridge.checkPluginHealth(id)
      const status = health.error?.match(/(?:returned|HTTP)\s+(\d{3})/i)?.[1]
      const endpoint = plugins.find((plugin) => plugin.id === id)?.endpoint_url ?? ''
      const result = health.healthy
        ? t('transcription.healthy')
        : status === '401' || status === '403'
          ? t('transcription.authError')
          : status
            ? t('transcription.httpError', { status })
            : health.error && /connect|refused|timeout|dial|fetch failed/i.test(health.error)
              ? t('transcription.unreachable', { url: endpoint })
              : t('transcription.unhealthy')
      setHealthResults((current) => ({ ...current, [purpose]: { id, message: result } }))
    } catch {
      setHealthResults((current) => ({
        ...current,
        [purpose]: { id, message: t('transcription.unhealthy') },
      }))
    } finally {
      setChecking(null)
      onBusyChange?.(false)
    }
  }

  async function save(operation: () => Promise<unknown>) {
    setSaving(true)
    onBusyChange?.(true)
    setError(null)
    setMessage(null)
    try {
      await operation()
      if (await load()) setMessage(t('common.saved'))
    } catch {
      setError(t('transcription.saveError'))
    } finally {
      setSaving(false)
      onBusyChange?.(false)
    }
  }

  return (
    <div className="space-y-5 text-start">
      <p className="text-sm leading-6 text-muted-foreground">{t('transcription.intro')}</p>
      {hosted !== null && (
        <p className="rounded-xl bg-muted/60 p-4 text-sm leading-6">
          {t(hosted ? 'transcription.hosted' : 'transcription.local')}
        </p>
      )}
      {loading && (
        <p role="status" className="text-sm text-muted-foreground">
          {t('common.loading')}
        </p>
      )}
      {!loading &&
        (['transcriber', 'streaming-transcriber'] as const).map((purpose) => {
          const choices = plugins.filter((p) => p.kind === purpose)
          const selected = choices.find((p) => p.is_default && p.enabled)
          const label = t(purpose === 'transcriber' ? 'transcription.batch' : 'transcription.live')
          const help = t(
            purpose === 'transcriber' ? 'transcription.batchHelp' : 'transcription.liveHelp'
          )
          const id = `transcription-${purpose}`
          return (
            <div key={purpose} className="space-y-2">
              <div className="flex items-center gap-1">
                <label htmlFor={id} className="text-sm font-medium">
                  {label}
                </label>
                <HelpTip label={label}>{help}</HelpTip>
              </div>
              <p id={`${id}-help`} className="text-sm leading-6 text-muted-foreground">
                {help}
              </p>
              <div className="flex flex-wrap items-center gap-2">
                <Select
                  id={id}
                  aria-describedby={`${id}-help`}
                  value={selected?.id ?? ''}
                  disabled={!canManage || busy || !!error}
                  onChange={(event) => {
                    const pluginId = event.target.value || null
                    void save(() => bridge.selectTranscriptionPlugin({ kind: purpose, pluginId }))
                  }}
                >
                  {purpose === 'streaming-transcriber' ? (
                    <option value="">{t('transcription.off')}</option>
                  ) : (
                    <option value="" disabled>
                      {t('transcription.choose')}
                    </option>
                  )}
                  {choices.map((p) => (
                    <option value={p.id} key={p.id}>
                      {name(p, hosted)}
                    </option>
                  ))}
                </Select>
                {selected && canManage && (
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    aria-label={uiText('{value1} — {value2}', {
                      value1: t(
                        checking === selected.id ? 'transcription.checking' : 'transcription.test'
                      ),
                      value2: label,
                    })}
                    disabled={busy}
                    onClick={() => {
                      void testConnection(purpose, selected.id)
                    }}
                  >
                    {t(checking === selected.id ? 'transcription.checking' : 'transcription.test')}
                  </Button>
                )}
              </div>
              {selected && healthResults[purpose]?.id === selected.id && (
                <p
                  data-testid={`${id}-health`}
                  role="status"
                  className="rounded-lg border border-border bg-muted/50 px-3 py-2 text-sm leading-6"
                >
                  {healthResults[purpose]?.message}
                </p>
              )}
              {!error && choices.length === 0 && purpose === 'transcriber' && (
                <p className="text-sm text-muted-foreground">{t('transcription.empty')}</p>
              )}
            </div>
          )
        })}
      {hosted === false && <TranscriptionModels bridge={bridge} />}
      {error && (
        <div role="alert" className="space-y-2 text-sm text-destructive">
          <p>{error}</p>
          <Button type="button" variant="secondary" disabled={busy} onClick={() => void load()}>
            {t('common.retry')}
          </Button>
        </div>
      )}
      <p role="status" className="text-sm text-muted-foreground">
        {saving ? t('common.saving') : message}
      </p>
      {canManage ? (
        <details className="rounded-xl border border-border p-4">
          <summary className="cursor-pointer text-sm font-medium">
            {t('transcription.advanced')}
          </summary>
          <form
            className="mt-4 space-y-3"
            onSubmit={(event) => {
              event.preventDefault()
              void save(async () => {
                await bridge.saveTranscriptionService({ kind, url: url.trim(), token })
                setUrl('')
                setToken('')
              })
            }}
          >
            <p className="text-sm leading-6 text-muted-foreground">
              {t('transcription.customHelp')}
            </p>
            <label className="flex flex-col gap-1 text-sm">
              {t('transcription.kind')}
              <Select
                value={kind}
                disabled={busy}
                onChange={(event) => setKind(event.target.value as TranscriberKind)}
              >
                <option value="transcriber">{t('transcription.batch')}</option>
                <option value="streaming-transcriber">{t('transcription.live')}</option>
              </Select>
            </label>
            <label className="flex flex-col gap-1 text-sm">
              {t('transcription.url')}
              <Input
                type="url"
                required
                value={url}
                disabled={busy}
                onChange={(event) => setUrl(event.target.value)}
                placeholder={t('transcription.urlPlaceholder')}
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              {t('transcription.token')}
              <Input
                type="password"
                value={token}
                disabled={busy}
                onChange={(event) => setToken(event.target.value)}
              />
            </label>
            <p className="text-xs leading-5 text-muted-foreground">
              {t('transcription.tokenHelp')}
            </p>
            <Button type="submit" disabled={busy || !url.trim()}>
              {saving ? t('common.saving') : t('common.save')}
            </Button>
          </form>
        </details>
      ) : (
        <p className="text-sm text-muted-foreground">{t('transcription.readOnly')}</p>
      )}
    </div>
  )
}
