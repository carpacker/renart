import { Outlet, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute(
  "/_workspace/settings/environments/$environmentId/connections"
)({
  component: EnvironmentConnectionsRouteComponent,
});

function EnvironmentConnectionsRouteComponent() {
  return <Outlet />;
}
