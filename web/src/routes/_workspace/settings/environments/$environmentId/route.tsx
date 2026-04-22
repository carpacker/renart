import { Outlet, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_workspace/settings/environments/$environmentId")({
  component: EnvironmentRouteComponent,
});

function EnvironmentRouteComponent() {
  return <Outlet />;
}
