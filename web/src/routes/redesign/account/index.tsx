import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/redesign/account/")({
  component: RedesignAccountIndexRoute,
});

function RedesignAccountIndexRoute() {
  return <Navigate to="/redesign/account/profile" />;
}
