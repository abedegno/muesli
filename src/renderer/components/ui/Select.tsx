import { forwardRef, type SelectHTMLAttributes } from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/cn'

/**
 * A native <select> restyled to match the app's custom dark-aware chrome
 * (border-input / bg-background / focus ring tokens) instead of the OS's
 * unstyled native dropdown. `appearance: none` strips the OS chrome while
 * keeping this a real, fully-accessible <select> — keyboard operable, with
 * its own focus ring, and still associable with a <label>.
 */
export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(
  ({ className, children, ...props }, ref) => (
    <span className="relative inline-flex">
      <select
        ref={ref}
        className={cn(
          'h-9 appearance-none rounded-[var(--radius)] border border-input bg-background pl-3 pr-8 text-sm text-foreground',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
          className,
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDown
        size={14}
        aria-hidden="true"
        className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground"
      />
    </span>
  ),
)
Select.displayName = 'Select'
