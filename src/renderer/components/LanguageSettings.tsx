import { useState } from 'react'
import { muesli } from '@/api'
import {
  useLocale,
  languageChoice,
  regionChoice,
  setDisplayLanguage,
  setRegion,
  uiText,
} from '@/i18n'
import { Select } from '@/components/ui/Select'

export function LanguageSettings({ compact = false }: { compact?: boolean }) {
  const [error, setError] = useState<string | null>(null)
  useLocale()
  return (
    <section className="mb-8 space-y-3" aria-labelledby={compact ? undefined : 'language-heading'}>
      {!compact && (
        <>
          <h2
            id="language-heading"
            className="text-sm font-medium uppercase tracking-wide text-muted-foreground"
          >
            {uiText('Language and region')}
          </h2>
          <p className="text-sm text-muted-foreground">
            {uiText(
              'Display language changes the app’s controls. Regional formatting changes dates and numbers. Neither changes the language of your recordings.'
            )}
          </p>
        </>
      )}
      <label className="flex flex-col gap-1 text-sm">
        {uiText('Display language')}
        <Select
          aria-label={uiText('Display language')}
          value={languageChoice()}
          onChange={(event) => {
            const language = event.target.value
            void Promise.resolve(muesli.setAppLanguage?.(language))
              .then(() => {
                setDisplayLanguage(language)
                setError(null)
              })
              .catch(() => setError('Could not save your display language. Try again.'))
          }}
        >
          <option value="system">{uiText('Follow system')}</option>
          <option value="en">{uiText('English')}</option>
          <option value="fr">{uiText('Français')}</option>
          <option value="de">{uiText('Deutsch')}</option>
          <option value="es">{uiText('Español')}</option>
        </Select>
      </label>
      {!compact && (
        <label className="flex flex-col gap-1 text-sm">
          {uiText('Regional formatting')}
          <Select
            aria-label={uiText('Regional formatting')}
            value={regionChoice()}
            onChange={(event) => setRegion(event.target.value)}
          >
            <option value="system">{uiText('Follow system')}</option>
            <option value="en-GB">{uiText('United Kingdom — 31/12/2026 · 1,234.5')}</option>
            <option value="en-US">{uiText('United States — 12/31/2026 · 1,234.5')}</option>
            <option value="fr-FR">{uiText('France — 31/12/2026 · 1 234,5')}</option>
            <option value="de-DE">{uiText('Deutschland — 31.12.2026 · 1.234,5')}</option>
            <option value="es-ES">{uiText('España — 31/12/2026 · 1234,5')}</option>
          </Select>
        </label>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {uiText(error)}
        </p>
      )}
      {!compact && (
        <p className="text-xs text-muted-foreground">
          {uiText('Time zones continue to follow your device.')}
        </p>
      )}
    </section>
  )
}
