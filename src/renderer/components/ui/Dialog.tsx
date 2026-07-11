import * as RDialog from '@radix-ui/react-dialog'
import type { ReactNode } from 'react'

export function Dialog({ open, onOpenChange, title, children }: {
  open: boolean
  onOpenChange: (o: boolean) => void
  title: string
  children: ReactNode
}) {
  return (
    <RDialog.Root open={open} onOpenChange={onOpenChange}>
      <RDialog.Portal>
        <RDialog.Overlay className="fixed inset-0 bg-black/40" />
        <RDialog.Content aria-describedby={undefined} className="fixed left-1/2 top-1/2 w-[32rem] max-w-[90vw] -translate-x-1/2 -translate-y-1/2 rounded-[var(--radius)] border border-border bg-card p-5 shadow-md focus:outline-none">
          <RDialog.Title className="mb-3 text-base font-semibold">{title}</RDialog.Title>
          {children}
        </RDialog.Content>
      </RDialog.Portal>
    </RDialog.Root>
  )
}
