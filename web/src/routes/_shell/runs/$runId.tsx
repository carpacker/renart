import { createFileRoute } from "@tanstack/react-router";

import { AppRunDetailPage, normalizeAppRunsSearch } from "@/components/app/runs-page";

export const Route = createFileRoute("/_shell/runs/$runId")({
  validateSearch: normalizeAppRunsSearch,
  component: AppRunDetailRoute,
});

function AppRunDetailRoute() {
  const { runId } = Route.useParams();
  const search = Route.useSearch();
  return <AppRunDetailPage runId={runId} search={search} />;
}
