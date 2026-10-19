import { useSyncExternalStore } from 'react'
import { en } from './en'
import { localeSnapshot, subscribeLocale, uiText } from '../../shared/browserLocale'
export * from '../../shared/browserLocale'
export { APP_LANGUAGES, type AppLanguage } from '../../shared/localization'
export type MessageKey = keyof typeof en
export function t(key: MessageKey, values: Record<string, string | number> = {}): string {
  return uiText(en[key], values)
}
export function useLocale() {
  return useSyncExternalStore(subscribeLocale, localeSnapshot, () => 'system|en|system')
}
