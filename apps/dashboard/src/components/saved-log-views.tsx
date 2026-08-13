import * as React from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { PencilIcon, PlusIcon, SaveIcon, Trash2Icon } from "lucide-react"

import {
  api,
  type SavedLogFilter,
  type SavedLogView,
  type SessionResponse,
} from "@/lib/api"
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
import { Skeleton } from "@/components/ui/skeleton"

const viewsKey = ["log-views"] as const

export function SavedLogViews({
  filter,
  onApply,
}: {
  filter: SavedLogFilter
  onApply: (filter: SavedLogFilter) => void
}) {
  const client = useQueryClient()
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<SessionResponse>("/auth/session"),
  })
  const views = useQuery({
    queryKey: viewsKey,
    queryFn: () => api<{ items: SavedLogView[] }>("/log-views"),
  })
  const [name, setName] = React.useState("")
  const [editing, setEditing] = React.useState<string | null>(null)
  const [editingName, setEditingName] = React.useState("")
  const headers = { "X-CSRF-Token": session.data?.csrfToken ?? "" }
  const refresh = () => client.invalidateQueries({ queryKey: viewsKey })
  const create = useMutation({
    mutationFn: () =>
      api<SavedLogView>("/log-views", {
        method: "POST",
        headers,
        body: JSON.stringify({ name, filter }),
      }),
    onSuccess: () => {
      setName("")
      refresh()
    },
  })
  const update = useMutation({
    mutationFn: (view: SavedLogView) =>
      api<SavedLogView>(`/log-views/${encodeURIComponent(view.id)}`, {
        method: "PUT",
        headers,
        body: JSON.stringify({
          name: editing === view.id ? editingName : view.name,
          filter: editing === view.id ? view.filter : filter,
          expectedRevision: view.revision,
        }),
      }),
    onSuccess: () => {
      setEditing(null)
      refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (view: SavedLogView) =>
      api<void>(
        `/log-views/${encodeURIComponent(view.id)}?expectedRevision=${view.revision}`,
        { method: "DELETE", headers }
      ),
    onSuccess: refresh,
  })
  const error = create.error ?? update.error ?? remove.error ?? views.error

  return (
    <Card>
      <CardHeader>
        <CardTitle>Saved journal views</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <FieldGroup className="flex flex-row flex-wrap items-end gap-2">
          <Field className="min-w-56 flex-1">
            <FieldLabel htmlFor="saved-log-view-name">
              Save current filters
            </FieldLabel>
            <Input
              id="saved-log-view-name"
              maxLength={128}
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Incident triage"
            />
          </Field>
          <Button
            disabled={!name.trim() || create.isPending || !session.data}
            onClick={() => create.mutate()}
          >
            <PlusIcon data-icon="inline-start" />
            Save view
          </Button>
        </FieldGroup>
        {error && (
          <Alert variant="destructive">
            <AlertTitle>Saved view action failed</AlertTitle>
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        )}
        {views.isPending && <Skeleton className="h-20" />}
        {!views.isPending && !views.isError && !views.data?.items.length && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No saved views</EmptyTitle>
              <EmptyDescription>
                Save the current filters for reuse.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        <div className="grid gap-2">
          {views.data?.items.map((view) => (
            <div
              key={view.id || view.name}
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
                    disabled={!editingName.trim() || update.isPending}
                    onClick={() => update.mutate(view)}
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
                  disabled={update.isPending}
                  onClick={() => update.mutate(view)}
                >
                  Update filters
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(view)}
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
