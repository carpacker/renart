import { createFileRoute } from "@tanstack/react-router";

import { RedesignDashboardPage } from "@/components/redesign/object-pages";

export const Route = createFileRoute("/redesign/dashboards/$dashboardId")({
  component: RedesignDashboardRoute,
});

function RedesignDashboardRoute() {
  const { dashboardId } = Route.useParams();
  return <RedesignDashboardPage dashboardId={dashboardId} />;
}
