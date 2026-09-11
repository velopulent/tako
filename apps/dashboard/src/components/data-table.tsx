import {
  type ColumnDef,
  type ColumnVisibilityState,
  columnFilteringFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  createFilteredRowModel,
  createSortedRowModel,
  filterFns,
  flexRender,
  globalFilteringFeature,
  type Header,
  type RowData,
  rowSortingFeature,
  type SortingState,
  sortFns,
  tableFeatures,
  useTable,
} from "@tanstack/react-table"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  ChevronDownIcon,
  ChevronsUpDownIcon,
  ChevronUpIcon,
  Columns3Icon,
  SearchIcon,
} from "lucide-react"
import * as React from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { cn } from "@/lib/utils"

const dataTableFeatures = tableFeatures({
  rowSortingFeature,
  columnFilteringFeature,
  globalFilteringFeature,
  columnVisibilityFeature,
  columnSizingFeature,
  filteredRowModel: createFilteredRowModel(),
  sortedRowModel: createSortedRowModel(),
  filterFns,
  sortFns,
})

type DataTableFeatures = typeof dataTableFeatures

export type { DataTableFeatures }

function columnSize(
  // biome-ignore lint/suspicious/noExplicitAny: TanStack Table v9 Header requires concrete generics; helper is type-erased by design
  header: Header<DataTableFeatures, any, any>
) {
  return header.getSize()
}

function headerLabel(header: string | undefined, id: string): string {
  return header ?? id
}

export function DataTable<T extends RowData>({
  data,
  columns,
  searchPlaceholder = "Search",
  height = "60vh",
  initialVisibility,
  onVisibilityChange,
  onRowClick,
  toolbar,
  search,
  onSearchChange,
  onScrollPosition,
}: {
  onScrollPosition?: (top: number) => void
  data: T[]
  columns: ColumnDef<DataTableFeatures, T>[]
  searchPlaceholder?: string
  height?: string
  initialVisibility?: ColumnVisibilityState
  onVisibilityChange?: (value: ColumnVisibilityState) => void
  onRowClick?: (row: T) => void
  toolbar?: React.ReactNode
  search?: string
  onSearchChange?: (value: string) => void
}) {
  const [sorting, setSorting] = React.useState<SortingState>([])
  const [uncontrolledFilter, setUncontrolledFilter] = React.useState("")
  const filter = search ?? uncontrolledFilter
  const setFilter = onSearchChange ?? setUncontrolledFilter
  const [visibility, setVisibility] = React.useState<ColumnVisibilityState>(
    initialVisibility ?? {}
  )
  const viewport = React.useRef<HTMLDivElement>(null)
  const table = useTable({
    features: dataTableFeatures,
    data,
    columns,
    defaultColumn: { size: 160, minSize: 72, maxSize: 640 },
    state: { sorting, globalFilter: filter, columnVisibility: visibility },
    onSortingChange: setSorting,
    onGlobalFilterChange: (updater) => {
      const next = typeof updater === "function" ? updater(filter) : updater
      setFilter(String(next ?? ""))
    },
    onColumnVisibilityChange: (updater) => {
      setVisibility((current: ColumnVisibilityState) => {
        const next = typeof updater === "function" ? updater(current) : updater
        onVisibilityChange?.(next)
        return next
      })
    },
  })
  const rows = table.getRowModel().rows
  const virtualized = rows.length > 40
  const virtualizer = useVirtualizer({
    enabled: virtualized,
    count: rows.length,
    getScrollElement: () => viewport.current,
    estimateSize: () => 42,
    overscan: 12,
    measureElement: (element) => element.getBoundingClientRect().height,
  })
  const virtualRows = virtualized
    ? virtualizer.getVirtualItems()
    : rows.map((_, index) => ({ index, start: 0 }))
  const totalSize = table.getTotalSize()
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <InputGroup className="min-w-56 flex-1">
          <InputGroupAddon>
            <SearchIcon aria-hidden="true" />
          </InputGroupAddon>
          <InputGroupInput
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={searchPlaceholder}
            aria-label={searchPlaceholder}
          />
        </InputGroup>
        {toolbar}
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>
            <Columns3Icon data-icon="inline-start" />
            Columns
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuGroup>
              <DropdownMenuLabel>Toggle columns</DropdownMenuLabel>
              {table
                .getAllLeafColumns()
                .filter((column) => column.getCanHide())
                .map((column) => (
                  <DropdownMenuCheckboxItem
                    key={column.id}
                    checked={column.getIsVisible()}
                    onCheckedChange={(checked) =>
                      column.toggleVisibility(Boolean(checked))
                    }
                  >
                    {headerLabel(
                      typeof column.columnDef.header === "string"
                        ? column.columnDef.header
                        : undefined,
                      column.id
                    )}
                  </DropdownMenuCheckboxItem>
                ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <ScrollArea
        viewportRef={viewport}
        onViewportScroll={(event) =>
          onScrollPosition?.(event.currentTarget.scrollTop)
        }
        className="rounded-lg border"
        style={{ height }}
      >
        <Table
          containerClassName="overflow-visible"
          style={{ minWidth: totalSize }}
        >
          <TableHeader className="sticky top-0 z-10 bg-background">
            {table.getHeaderGroups().map((group) => (
              <TableRow
                key={group.id}
                className="flex w-full"
                style={{ minWidth: totalSize }}
              >
                {group.headers.map((header) => {
                  const align = header.column.columnDef.meta?.align ?? "start"
                  return (
                    <TableHead
                      key={header.id}
                      className={cn(
                        "flex h-10 min-w-0 items-center overflow-hidden",
                        align === "end" && "text-right"
                      )}
                      style={{
                        width: columnSize(
                          // biome-ignore lint/suspicious/noExplicitAny: type-erased header passed to the generic columnSize helper
                          header as Header<DataTableFeatures, any, any>
                        ),
                        flex: `${header.getSize()} 0 auto`,
                      }}
                    >
                      <Button
                        variant="ghost"
                        size="sm"
                        className={cn(
                          "w-full",
                          align === "end" ? "justify-end" : "justify-start"
                        )}
                        disabled={!header.column.getCanSort()}
                        onClick={header.column.getToggleSortingHandler()}
                      >
                        {flexRender(
                          header.column.columnDef.header,
                          header.getContext()
                        )}
                        {header.column.getCanSort() &&
                          (header.column.getIsSorted() === "asc" ? (
                            <ChevronUpIcon data-icon="inline-end" />
                          ) : header.column.getIsSorted() === "desc" ? (
                            <ChevronDownIcon data-icon="inline-end" />
                          ) : (
                            <ChevronsUpDownIcon data-icon="inline-end" />
                          ))}
                      </Button>
                    </TableHead>
                  )
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody
            style={{
              height: virtualized ? `${virtualizer.getTotalSize()}px` : "auto",
              position: "relative",
            }}
          >
            {virtualRows.map((virtualRow) => {
              const row = rows[virtualRow.index]
              return (
                <TableRow
                  key={row.id}
                  data-index={virtualRow.index}
                  ref={(node) => {
                    if (virtualized) virtualizer.measureElement(node)
                  }}
                  tabIndex={onRowClick ? 0 : undefined}
                  className={cn("flex", onRowClick && "cursor-pointer")}
                  onClick={() => onRowClick?.(row.original)}
                  onKeyDown={(event) => {
                    if (
                      onRowClick &&
                      (event.key === "Enter" || event.key === " ")
                    )
                      onRowClick(row.original)
                  }}
                  style={{
                    position: virtualized ? "absolute" : "relative",
                    transform: virtualized
                      ? `translateY(${virtualRow.start}px)`
                      : undefined,
                    width: "100%",
                    minWidth: totalSize,
                    display: "flex",
                  }}
                >
                  {row.getVisibleCells().map((cell) => {
                    const align = cell.column.columnDef.meta?.align ?? "start"
                    const wrap = cell.column.columnDef.meta?.wrap === true
                    return (
                      <TableCell
                        key={cell.id}
                        className={cn(
                          "flex min-w-0 items-center overflow-hidden",
                          wrap ? "whitespace-normal" : "whitespace-nowrap",
                          align === "end" &&
                            "justify-end text-right tabular-nums"
                        )}
                        style={{
                          width: cell.column.getSize(),
                          flex: `${cell.column.getSize()} 0 auto`,
                        }}
                      >
                        {wrap ? (
                          flexRender(
                            cell.column.columnDef.cell,
                            cell.getContext()
                          )
                        ) : (
                          <div className="min-w-0 truncate">
                            {flexRender(
                              cell.column.columnDef.cell,
                              cell.getContext()
                            )}
                          </div>
                        )}
                      </TableCell>
                    )
                  })}
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        {rows.length === 0 && (
          <p className="p-10 text-center text-sm text-muted-foreground">
            No matching results.
          </p>
        )}
      </ScrollArea>
    </div>
  )
}
