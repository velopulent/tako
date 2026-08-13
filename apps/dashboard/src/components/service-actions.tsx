import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import {
  serviceActionNames,
  type ServiceActionName,
} from "@/lib/service-actions"

type ServiceActionsProps = {
  scope: string
  unit: string
  administrative: boolean
  pending: boolean
  onAction: (name: ServiceActionName) => void
}

export function ServiceActions({
  scope,
  unit,
  administrative,
  pending,
  onAction,
}: ServiceActionsProps) {
  const needsAdministrative = scope === "system"
  const authorityReady = !needsAdministrative || administrative
  const authorityMessage = needsAdministrative
    ? "Gain Administrative access before changing a system unit."
    : "The action runs as your authenticated UNIX user."

  return (
    <div className="flex flex-wrap gap-2">
      {serviceActionNames.map((name) => (
        <AlertDialog key={name}>
          <AlertDialogTrigger
            render={
              <Button
                variant={
                  name === "stop" || name === "mask" ? "destructive" : "outline"
                }
                size="sm"
                disabled={!authorityReady || pending}
              />
            }
          >
            {name[0].toUpperCase() + name.slice(1)}
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {name} {unit}?
              </AlertDialogTitle>
              <AlertDialogDescription>
                {authorityMessage} This changes {scope} systemd state.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                disabled={pending}
                onClick={() => onAction(name)}
              >
                Confirm
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      ))}
    </div>
  )
}
