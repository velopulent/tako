import type { RowData } from "@tanstack/react-table"

declare module "@tanstack/react-table" {
  interface ColumnMeta<TFeatures, TData extends RowData, TValue> {
    align?: "start" | "end"
    wrap?: boolean
  }
}
