import { readFileSync, writeFileSync, renameSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { APP_LANGUAGES, translate, type AppLanguage } from '../shared/localization'
export class LocaleSettings {
  constructor(
    private readonly directory: string,
    private readonly systemLocale: () => string
  ) {}
  private choice(): string {
    try {
      const data = JSON.parse(readFileSync(join(this.directory, 'ui-language.json'), 'utf8')) as {
        language: string
      }
      return data.language
    } catch {
      return 'system'
    }
  }
  private language(): AppLanguage {
    const choice = this.choice(),
      value = (choice === 'system' ? this.systemLocale() : choice).split('-')[0]
    return APP_LANGUAGES.includes(value as AppLanguage) ? (value as AppLanguage) : 'en'
  }
  text(source: string, values: Record<string, unknown> = {}): string {
    return translate(this.language(), source, values)
  }
  set(language: string): void {
    if (language !== 'system' && !APP_LANGUAGES.includes(language as AppLanguage))
      throw new Error('Choose a supported app language.')
    mkdirSync(this.directory, { recursive: true, mode: 0o700 })
    const target = join(this.directory, 'ui-language.json'),
      temp = target + '.tmp'
    writeFileSync(temp, JSON.stringify({ language }), { mode: 0o600 })
    renameSync(temp, target)
  }
}
