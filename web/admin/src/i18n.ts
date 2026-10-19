import { useSyncExternalStore } from 'react'
import { localeSnapshot, subscribeLocale } from '../../../src/shared/browserLocale'
export * from '../../../src/shared/browserLocale'
export function useLocale() {
  return useSyncExternalStore(subscribeLocale, localeSnapshot, () => 'system|en|system')
}
