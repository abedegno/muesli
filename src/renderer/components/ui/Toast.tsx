import { uiText } from '@/i18n'
import * as RToast from '@radix-ui/react-toast'
import { createContext, useCallback, useContext, useState, useRef, type ReactNode } from 'react'

interface ToastMsg {
  id: number
  title: string
  tone?: 'error' | 'info'
}
interface ToastApi {
  notify: (title: string, tone?: 'error' | 'info') => void
}
const Ctx = createContext<ToastApi>({ notify: () => {} })
export const useToast = () => useContext(Ctx)

export function ToastProvider({ children }: { children: ReactNode }) {
  const nextId = useRef(1)
  const [msgs, setMsgs] = useState<ToastMsg[]>([])
  const notify = useCallback((title: string, tone: 'error' | 'info' = 'info') => {
    const id = nextId.current++
    setMsgs((m) => [...m.slice(-19), { id, title, tone }])
  }, [])
  return (
    <Ctx.Provider value={{ notify }}>
      <RToast.Provider duration={4000}>
        {children}
        {msgs.map((m) => (
          <RToast.Root
            key={m.id}
            onOpenChange={(open) => !open && setMsgs((x) => x.filter((t) => t.id !== m.id))}
            className="rounded-[var(--radius)] border border-border bg-popover px-4 py-3 text-sm shadow-md"
            role="alert"
          >
            <RToast.Title className={m.tone === 'error' ? 'text-destructive' : 'text-foreground'}>
              {uiText(m.title)}
            </RToast.Title>
          </RToast.Root>
        ))}
        <RToast.Viewport className="fixed bottom-4 right-4 flex w-80 flex-col gap-2 outline-none" />
      </RToast.Provider>
    </Ctx.Provider>
  )
}
