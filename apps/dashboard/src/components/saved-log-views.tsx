import * as React from "react"
import { PencilIcon, PlusIcon, SaveIcon, Trash2Icon } from "lucide-react"

import type { SavedLogFilter } from "@/lib/api"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"

type LocalView = {
  id: string
  name: string
  filter: SavedLogFilter
  createdAt: string
  updatedAt: string
}

const storageKey = "tako-log-views:v1"

function readViews(): LocalView[] {
  try {
    const raw = localStorage.getItem(storageKey)
    if (!raw) return []
    const parsed = JSON.parse(raw) as LocalView[]
    if (!Array.isArray(parsed)) return []
    return parsed.filter((view) => view && view.id && view.name)
  } catch {
    return []
  }
}

function writeViews(views: LocalView[]) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(views))
  } catch {
    // ignore
  }
}

export function SavedLogViews({
  filter,
  onApply,
}: {
  filter: SavedLogFilter
  onApply: (filter: SavedLogFilter) => void
}) {
  const [views, setViews] = React.useState<LocalView[]>(readViews)
  const [name, setName] = React.useState("")
  const [editing, setEditing] = React.useState<string | null>(null)
  const [editingName, setEditingName] = React.useState("")
  const [error, setError] = React.useState<string | null>(null)

  const persist = (next: LocalView[]) => {
    setViews(next)
    writeViews(next)
  }

  const create = () => {
    const trimmed = name.trim()
    if (!trimmed) return
    if (views.some((view) => view.name.toLowerCase() === trimmed.toLowerCase())) {
      setError("A view with this name already exists in this browser.")
      return
    }
    setError(null)
    const now = new Date().toISOString()
    persist([
      ...views,
      {
        id: crypto.randomUUID(),
        name: trimmed,
        filter,
        createdAt: now,
        updatedAt: now,
      },
    ])
    setName("")
  }

  const rename = (view: LocalView) => {
    const trimmed = editingName.trim()
    if (!trimmed) return
    setError(null)
    persist(
      views.map((item) =>
        item.id === view.id
          ? { ...item, name: trimmed, updatedAt: new Date().toISOString() }
          : item
      )
    )
    setEditing(null)
  }

  const updateFilters = (view: LocalView) => {
    setError(null)
    persist(
      views.map((item) =>
        item.id === view.id
          ? { ...item, filter, updatedAt: new Date().toISOString() }
          : item
      )
    )
  }

  const remove = (view: LocalView) => {
    setError(null)
    persist(views.filter((item) => item.id !== view.id))
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Saved journal views</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <FieldGroup className="flex flex-row flex-wrap items-end gap-2">
          <Field className="min-w-56 flex-1">
            <FieldLabel htmlFor="saved-log-view-name">
              Save current filters in this browser
            </FieldLabel>
            <Input
              id="saved-log-view-name"
              maxLength={128}
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Incident triage"
            />
          </Field>
          <Button disabled={!name.trim()} onClick={create}>
            <PlusIcon data-icon="inline-start" />
            Save view
          </Button>
        </FieldGroup>
        {error && (
          <Alert variant="destructive">
            <AlertTitle>Saved view action failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
        {!views.length && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No saved views</EmptyTitle>
              <EmptyDescription>
                Save the current filters for reuse in this browser.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        <div className="grid gap-2">
          {views.map((view) => (
            <div
              key={view.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded-lg border p-3"
            >
              {editing === view.id ? (
                <Input
                  aria-label={`Rename ${view.name}`}
                  maxLength={128}
                  value={editingName}
                  onChange={(event) => setEditingName(event.target.value)}
                  className="min-w-48 flex-1"
                />
              ) : (
                <span className="font-medium">{view.name}</span>
              )}
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => onApply(view.filter)}
                >
                  Apply
                </Button>
                {editing === view.id ? (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!editingName.trim()}
                    onClick={() => rename(view)}
                  >
                    <SaveIcon data-icon="inline-start" />
                    Save name
                  </Button>
                ) : (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setEditing(view.id)
                      setEditingName(view.name)
                    }}
                  >
                    <PencilIcon data-icon="inline-start" />
                    Rename
                  </Button>
                )}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => updateFilters(view)}
                >
                  Update filters
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => remove(view)}
                >
                  <Trash2Icon data-icon="inline-start" />
                  Delete
                </Button>
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
