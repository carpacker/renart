import { Outlet, createFileRoute } from "@tanstack/react-router";

import { WorkspaceEnvironmentsHub } from "@/components/workspace-environments-hub";

export const Route = createFileRoute("/_workspace/settings/environments")({
  component: EnvironmentsRouteComponent,
});

function EnvironmentsRouteComponent() {
  return (
    <>
      <WorkspaceEnvironmentsHub />
      <Outlet />
    </>
  );
}
