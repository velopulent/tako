import type { UpdatePackage } from "@/lib/api"
import { Checkbox } from "@/components/ui/checkbox"

function bytes(value: number) {
  if (!value) return "-"
  const units = ["B", "KiB", "MiB", "GiB"]
  let amount = value
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index++
  }
  return `${amount.toFixed(index ? 1 : 0)} ${units[index]}`
}

export function UpdatePackageTable({
  packages,
  selected,
  onToggle,
}: {
  packages: UpdatePackage[]
  selected: string[]
  onToggle: (name: string) => void
}) {
  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full min-w-[52rem] text-sm">
        <thead className="bg-muted/50 text-left">
          <tr>
            <th className="w-10 px-3 py-2">
              <span className="sr-only">Select</span>
            </th>
            <th className="px-3 py-2 font-medium">Package</th>
            <th className="px-3 py-2 font-medium">Installed</th>
            <th className="px-3 py-2 font-medium">Available</th>
            <th className="px-3 py-2 font-medium">Severity</th>
            <th className="px-3 py-2 font-medium">Size</th>
            <th className="px-3 py-2 font-medium">Summary</th>
          </tr>
        </thead>
        <tbody>
          {packages.map((item) => {
            const checked = selected.includes(item.name)
            return (
              <tr
                key={`${item.name}-${item.architecture ?? ""}`}
                className="border-t"
              >
                <td className="px-3 py-2">
                  <Checkbox
                    aria-label={`Select ${item.name}`}
                    checked={checked}
                    onCheckedChange={() => onToggle(item.name)}
                  />
                </td>
                <td className="px-3 py-2 font-medium">
                  {item.name}
                  {item.architecture && ` (${item.architecture})`}
                </td>
                <td className="px-3 py-2">{item.currentVersion || "-"}</td>
                <td className="px-3 py-2">{item.candidateVersion}</td>
                <td className="px-3 py-2">{item.severity || "-"}</td>
                <td className="px-3 py-2">{bytes(item.size ?? 0)}</td>
                <td className="max-w-sm px-3 py-2">
                  {item.summary || item.details || "-"}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
