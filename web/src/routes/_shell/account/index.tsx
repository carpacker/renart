import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/account/")({
  component: AppAccountIndexRoute,
});

function AppAccountIndexRoute() {
  return <Navigate to="/account/profile" />;
}
