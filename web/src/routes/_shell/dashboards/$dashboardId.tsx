import { createFileRoute } from "@tanstack/react-router";

import { AppDashboardPage } from "@/components/app/object-pages";

export const Route = createFileRoute("/_shell/dashboards/$dashboardId")({
  component: AppDashboardRoute,
});

function AppDashboardRoute() {
  const { dashboardId } = Route.useParams();
  return <AppDashboardPage dashboardId={dashboardId} />;
}
