import * as RDropdownMenu from '@radix-ui/react-dropdown-menu'
import { forwardRef } from 'react'
import { cn } from '@/lib/cn'

// Thin owned wrapper over @radix-ui/react-dropdown-menu (mirrors ui/ContextMenu.tsx).
export const DropdownMenu = RDropdownMenu.Root
export const DropdownMenuTrigger = RDropdownMenu.Trigger

export const DropdownMenuContent = forwardRef<
  React.ElementRef<typeof RDropdownMenu.Content>,
  React.ComponentPropsWithoutRef<typeof RDropdownMenu.Content>
>(({ className, ...props }, ref) => (
  <RDropdownMenu.Portal>
    <RDropdownMenu.Content
      ref={ref}
      className={cn(
        'z-50 min-w-[10rem] overflow-hidden rounded-[var(--radius)] border border-border bg-popover p-1 text-sm shadow-md',
        className,
      )}
      {...props}
    />
  </RDropdownMenu.Portal>
))
DropdownMenuContent.displayName = 'DropdownMenuContent'

export const DropdownMenuItem = forwardRef<
  React.ElementRef<typeof RDropdownMenu.Item>,
  React.ComponentPropsWithoutRef<typeof RDropdownMenu.Item> & { destructive?: boolean }
>(({ className, destructive, ...props }, ref) => (
  <RDropdownMenu.Item
    ref={ref}
    className={cn(
      'flex cursor-pointer items-center gap-2 rounded-[calc(var(--radius)-2px)] px-2 py-1.5 outline-none data-[highlighted]:bg-muted',
      destructive && 'text-destructive',
      className,
    )}
    {...props}
  />
))
DropdownMenuItem.displayName = 'DropdownMenuItem'

export const DropdownMenuSeparator = forwardRef<
  React.ElementRef<typeof RDropdownMenu.Separator>,
  React.ComponentPropsWithoutRef<typeof RDropdownMenu.Separator>
>(({ className, ...props }, ref) => (
  <RDropdownMenu.Separator ref={ref} className={cn('my-1 h-px bg-border', className)} {...props} />
))
DropdownMenuSeparator.displayName = 'DropdownMenuSeparator'
