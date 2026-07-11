import * as RContextMenu from '@radix-ui/react-context-menu'
import { ChevronRight } from 'lucide-react'
import { forwardRef } from 'react'
import { cn } from '@/lib/cn'

// Thin owned wrapper over @radix-ui/react-context-menu (mirrors ui/Dialog.tsx).
export const ContextMenu = RContextMenu.Root
export const ContextMenuTrigger = RContextMenu.Trigger
export const ContextMenuSub = RContextMenu.Sub

export const ContextMenuContent = forwardRef<
  React.ElementRef<typeof RContextMenu.Content>,
  React.ComponentPropsWithoutRef<typeof RContextMenu.Content>
>(({ className, ...props }, ref) => (
  <RContextMenu.Portal>
    <RContextMenu.Content
      ref={ref}
      className={cn(
        'z-50 min-w-[10rem] overflow-hidden rounded-[var(--radius)] border border-border bg-popover p-1 text-sm shadow-md',
        className,
      )}
      {...props}
    />
  </RContextMenu.Portal>
))
ContextMenuContent.displayName = 'ContextMenuContent'

export const ContextMenuItem = forwardRef<
  React.ElementRef<typeof RContextMenu.Item>,
  React.ComponentPropsWithoutRef<typeof RContextMenu.Item> & { destructive?: boolean }
>(({ className, destructive, ...props }, ref) => (
  <RContextMenu.Item
    ref={ref}
    className={cn(
      'flex cursor-pointer items-center gap-2 rounded-[calc(var(--radius)-2px)] px-2 py-1.5 outline-none data-[highlighted]:bg-muted',
      destructive && 'text-destructive',
      className,
    )}
    {...props}
  />
))
ContextMenuItem.displayName = 'ContextMenuItem'

export const ContextMenuSeparator = forwardRef<
  React.ElementRef<typeof RContextMenu.Separator>,
  React.ComponentPropsWithoutRef<typeof RContextMenu.Separator>
>(({ className, ...props }, ref) => (
  <RContextMenu.Separator ref={ref} className={cn('my-1 h-px bg-border', className)} {...props} />
))
ContextMenuSeparator.displayName = 'ContextMenuSeparator'

export const ContextMenuSubTrigger = forwardRef<
  React.ElementRef<typeof RContextMenu.SubTrigger>,
  React.ComponentPropsWithoutRef<typeof RContextMenu.SubTrigger>
>(({ className, children, ...props }, ref) => (
  <RContextMenu.SubTrigger
    ref={ref}
    className={cn(
      'flex cursor-pointer items-center gap-2 rounded-[calc(var(--radius)-2px)] px-2 py-1.5 outline-none data-[highlighted]:bg-muted data-[state=open]:bg-muted',
      className,
    )}
    {...props}
  >
    {children}
    <ChevronRight className="ml-auto h-4 w-4" aria-hidden />
  </RContextMenu.SubTrigger>
))
ContextMenuSubTrigger.displayName = 'ContextMenuSubTrigger'

export const ContextMenuSubContent = forwardRef<
  React.ElementRef<typeof RContextMenu.SubContent>,
  React.ComponentPropsWithoutRef<typeof RContextMenu.SubContent>
>(({ className, ...props }, ref) => (
  <RContextMenu.Portal>
    <RContextMenu.SubContent
      ref={ref}
      className={cn(
        'z-50 min-w-[10rem] overflow-hidden rounded-[var(--radius)] border border-border bg-popover p-1 text-sm shadow-md',
        className,
      )}
      {...props}
    />
  </RContextMenu.Portal>
))
ContextMenuSubContent.displayName = 'ContextMenuSubContent'
