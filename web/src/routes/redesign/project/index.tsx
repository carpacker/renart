import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/redesign/project/")({
  component: RedesignProjectIndexRoute,
});

function RedesignProjectIndexRoute() {
  return <Navigate to="/redesign/project/general" />;
}
