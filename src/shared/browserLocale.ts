import { APP_LANGUAGES, translate, type AppLanguage } from './localization'
const LANGUAGE_KEY = 'muesli-language',
  REGION_KEY = 'muesli-region'
function stored(key: string): string | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage.getItem(key)
  } catch {
    return null
  }
}
export function languageChoice(): string {
  return stored(LANGUAGE_KEY) ?? 'system'
}
export function getDisplayLanguage(): AppLanguage {
  const choice = languageChoice()
  const candidate =
    choice === 'system'
      ? (typeof navigator === 'undefined' ? 'en' : navigator.language).split('-')[0]
      : choice
  return APP_LANGUAGES.includes(candidate as AppLanguage) ? (candidate as AppLanguage) : 'en'
}
export function regionChoice(): string {
  return stored(REGION_KEY) ?? 'system'
}
export function getRegion(): string | undefined {
  const region = regionChoice()
  return region === 'system' ? undefined : region
}
function changed() {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event('muesli:locale-changed'))
}
export function setDisplayLanguage(language: string) {
  if (language !== 'system' && !APP_LANGUAGES.includes(language as AppLanguage))
    throw new Error('Choose a supported display language.')
  localStorage.setItem(LANGUAGE_KEY, language)
  changed()
}
export function setRegion(region: string) {
  if (region !== 'system') new Intl.DateTimeFormat(region)
  localStorage.setItem(REGION_KEY, region)
  changed()
}
export function uiText(source: string, values: Record<string, unknown> = {}): string {
  return translate(getDisplayLanguage(), source, values)
}
export function formatNumber(
  value: number,
  options: Intl.NumberFormatOptions = {},
  locale?: string
): string {
  return new Intl.NumberFormat(locale ?? getRegion(), options).format(value)
}
export function formatDate(
  value: Date,
  options: Intl.DateTimeFormatOptions,
  locale?: string
): string {
  return new Intl.DateTimeFormat(locale ?? getRegion(), options).format(value)
}
export function localeSnapshot() {
  return `${languageChoice()}|${getDisplayLanguage()}|${regionChoice()}`
}
export function subscribeLocale(listener: () => void) {
  if (typeof window === 'undefined') return () => {}
  const events = ['muesli:locale-changed', 'storage', 'languagechange']
  for (const event of events) window.addEventListener(event, listener)
  return () => {
    for (const event of events) window.removeEventListener(event, listener)
  }
}
