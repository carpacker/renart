import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_workspace/settings/environments/")({
  component: WorkspaceSettingsEnvironmentsRouteComponent,
});

function WorkspaceSettingsEnvironmentsRouteComponent() {
  return null;
}
