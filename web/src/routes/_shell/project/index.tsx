import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/project/")({
  component: AppProjectIndexRoute,
});

function AppProjectIndexRoute() {
  return <Navigate to="/project/general" />;
}
