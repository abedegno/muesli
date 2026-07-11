import * as ToggleGroup from '@radix-ui/react-toggle-group'
import { cn } from '@/lib/cn'

interface SegmentedOption {
  value: string
  label: string
}

export function SegmentedControl({
  options,
  value,
  onValueChange,
  ariaLabel,
}: {
  options: SegmentedOption[]
  value: string
  onValueChange: (v: string) => void
  ariaLabel: string
}) {
  return (
    <ToggleGroup.Root
      type="single"
      value={value}
      onValueChange={(v) => v && onValueChange(v)}
      aria-label={ariaLabel}
      className="inline-flex gap-1 rounded-[var(--radius)] bg-muted p-1"
    >
      {options.map((o) => (
        <ToggleGroup.Item
          key={o.value}
          value={o.value}
          className={cn(
            'rounded-[calc(var(--radius)-2px)] px-3 py-1 text-sm font-medium text-muted-foreground',
            'data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-sm',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
          )}
        >
          {o.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  )
}
