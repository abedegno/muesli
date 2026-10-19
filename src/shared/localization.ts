import english from '../renderer/i18n/messages.json'
import french from '../renderer/i18n/fr.json'
import german from '../renderer/i18n/de.json'
import spanish from '../renderer/i18n/es.json'

export const APP_LANGUAGES = ['en', 'fr', 'de', 'es'] as const
export type AppLanguage = (typeof APP_LANGUAGES)[number]
export const catalogues: Record<AppLanguage, Record<string, string>> = {
  en: english,
  fr: french,
  de: german,
  es: spanish,
}
export function translate(
  language: AppLanguage,
  source: string,
  values: Record<string, unknown> = {}
): string {
  const message = catalogues[language][source] ?? source
  return message.replace(/\{(\w+)\}/g, (whole, name: string) =>
    values[name] === undefined ? whole : String(values[name])
  )
}
