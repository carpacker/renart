import { createFileRoute } from "@tanstack/react-router";

import { WorkspaceLayout } from "@/components/workspace-layout";

export const Route = createFileRoute("/_workspace")({
  component: WorkspaceRouteComponent,
});

function WorkspaceRouteComponent() {
  return <WorkspaceLayout />;
}
