import { Outlet, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute(
  "/_workspace/settings/environments/$environmentId/connections/$connectionId"
)({
  component: ConnectionRouteComponent,
});

function ConnectionRouteComponent() {
  return <Outlet />;
}
