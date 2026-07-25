import { forwardRef, type InputHTMLAttributes } from 'react'
import { Check } from 'lucide-react'
import { cn } from '@/lib/cn'

/**
 * A native <input type="checkbox"> restyled to match the app's custom
 * dark-aware chrome instead of the OS's default light checkbox.
 * `appearance: none` strips the OS chrome while keeping this a real,
 * fully-accessible checkbox — keyboard operable, with its own focus ring,
 * and still associable with a <label>. The checkmark is a sibling icon shown
 * via the `peer-checked` variant, so no pseudo-element content is needed.
 */
export const Checkbox = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <span className="relative inline-flex h-4 w-4 shrink-0 items-center justify-center">
      <input
        ref={ref}
        type="checkbox"
        className={cn(
          'peer h-4 w-4 shrink-0 appearance-none rounded-[calc(var(--radius)-2px)] border border-input bg-background',
          'checked:border-primary checked:bg-primary',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background',
          className,
        )}
        {...props}
      />
      <Check
        size={12}
        strokeWidth={3}
        aria-hidden="true"
        className="pointer-events-none absolute text-primary-foreground opacity-0 peer-checked:opacity-100"
      />
    </span>
  ),
)
Checkbox.displayName = 'Checkbox'
