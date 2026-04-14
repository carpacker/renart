import { createFileRoute } from "@tanstack/react-router";

import { WorkspaceSettingsLayout } from "@/components/workspace-settings-layout";

export const Route = createFileRoute("/_workspace/settings")({
  component: WorkspaceSettingsRouteComponent,
});

function WorkspaceSettingsRouteComponent() {
  return <WorkspaceSettingsLayout />;
}
