import { useRef, useState } from 'react'
import * as Tooltip from '@radix-ui/react-tooltip'
import { Info } from 'lucide-react'
import { t } from '@/i18n'

export function HelpTip({ label, children }: { label: string; children: React.ReactNode }) {
  const trigger = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  return (
    <Tooltip.Provider delayDuration={200}>
      <Tooltip.Root
        open={open}
        onOpenChange={(next) => {
          // Radix dismisses on ancestor scroll, including the browser scrolling a
          // newly keyboard-focused trigger into view. Keep focused help readable;
          // Escape, blur and outside presses still dismiss it explicitly.
          if (next || document.activeElement !== trigger.current) setOpen(next)
        }}
      >
        <Tooltip.Trigger asChild>
          <button
            ref={trigger}
            type="button"
            onClick={() => setOpen(true)}
            aria-label={t('help.label', { label })}
            className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <Info aria-hidden="true" className="h-4 w-4" />
          </button>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            onEscapeKeyDown={() => setOpen(false)}
            onPointerDownOutside={() => setOpen(false)}
            sideOffset={6}
            collisionPadding={12}
            className="z-50 max-w-xs rounded-[var(--radius)] border border-border bg-popover px-3 py-2 text-start text-sm leading-6 text-foreground shadow-md"
          >
            {children}
            <Tooltip.Arrow className="fill-popover" />
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  )
}
