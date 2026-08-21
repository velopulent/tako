import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { refreshIntervals, type RefreshInterval } from "@/lib/monitoring"

export function RefreshSelect({
  value,
  onChange,
}: {
  value: RefreshInterval
  onChange: (value: RefreshInterval) => void
}) {
  return (
    <Select
      items={refreshIntervals.map((item) => ({
        value: item.value,
        label: item.label,
      }))}
      value={value}
      onValueChange={(next) => onChange(next as RefreshInterval)}
    >
      <SelectTrigger size="sm" aria-label="Refresh interval">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {refreshIntervals.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
