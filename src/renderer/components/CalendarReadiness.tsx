import { uiText } from '@/i18n'
import { useEffect, useState } from 'react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { formatDate } from '@/i18n'
import type { CalendarReadiness as Readiness } from '../../shared/types'

export function CalendarReadiness({ hosted }: { hosted: boolean | null }) {
  const [state, setState] = useState<Readiness | null>(null)
  const [error, setError] = useState(false)
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    let active = true
    if (typeof muesli.getCalendarReadiness !== 'function') return
    void muesli
      .getCalendarReadiness()
      .then((next) => {
        if (active) {
          setState(next)
          setError(false)
        }
      })
      .catch(() => {
        if (active) setError(true)
      })
    const recheck = () => setRefresh((value) => value + 1)
    window.addEventListener('focus', recheck)
    return () => {
      active = false
      window.removeEventListener('focus', recheck)
    }
  }, [refresh, hosted])
  if (!state && !error) return null
  return (
    <div className="mt-3 space-y-3 rounded-lg border border-border p-4 text-sm" role="status">
      {error ? (
        <p>
          {uiText('Could not check calendar setup. Try again before changing your connections.')}
        </p>
      ) : (
        state && (
          <>
            {state.connections > 0 ? (
              <>
                <p>
                  {state.failures > 0
                    ? uiText(
                        'A calendar connection needs attention. Reconnect its account to resume syncing.'
                      )
                    : uiText('{value1} calendar connection{value2} available.', {
                        value1: state.connections,
                        value2: state.connections === 1 ? '' : 's',
                      })}
                </p>
                <p className="text-muted-foreground">
                  {state.lastSyncedAt
                    ? uiText('Last synced: {value1}', {
                        value1: formatDate(new Date(state.lastSyncedAt), {
                          dateStyle: 'medium',
                          timeStyle: 'short',
                        }),
                      })
                    : uiText('The first calendar sync has not finished yet.')}
                </p>
              </>
            ) : state.googleConfigured || state.microsoftConfigured ? (
              <p>
                {uiText(
                  'Calendar sign-in is available. Use a Connect button below, finish sign-in in your browser, then re-check here.'
                )}
              </p>
            ) : (
              <>
                <p>
                  {hosted
                    ? uiText(
                        'Calendar sign-in is not configured on this team server. Ask your administrator to enable Google or Microsoft Calendar.'
                      )
                    : uiText(
                        'Calendar sign-in needs provider setup on your built-in server. It is optional; you can record any meeting manually.'
                      )}
                </p>
                <details>
                  <summary className="cursor-pointer font-medium">
                    {uiText('Calendar setup guidance')}
                  </summary>
                  <p className="mt-2 text-muted-foreground">
                    {hosted
                      ? uiText(
                          'Provider credentials belong on the team server, not on each person’s device.'
                        )
                      : uiText(
                          'Google and Microsoft require an OAuth application and callback URL. Existing ICS or CalDAV connections also work without OAuth.'
                        )}
                  </p>
                  <a
                    className="mt-2 inline-block underline"
                    href="https://github.com/abedegno/muesli/blob/main/docs/CALENDAR_OAUTH_SETUP.md"
                    target="_blank"
                    rel="noreferrer"
                  >
                    {uiText('Calendar setup guide')}
                  </a>
                </details>
              </>
            )}
          </>
        )
      )}
      <Button size="sm" variant="secondary" onClick={() => setRefresh((value) => value + 1)}>
        {uiText('Re-check calendar')}
      </Button>
    </div>
  )
}
