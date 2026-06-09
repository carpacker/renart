import { createFileRoute } from "@tanstack/react-router";

import { RedesignRunDetailPage, normalizeRedesignRunsSearch } from "@/components/redesign/runs-page";

export const Route = createFileRoute("/redesign/runs/$runId")({
  validateSearch: normalizeRedesignRunsSearch,
  component: RedesignRunDetailRoute,
});

function RedesignRunDetailRoute() {
  const { runId } = Route.useParams();
  const search = Route.useSearch();
  return <RedesignRunDetailPage runId={runId} search={search} />;
}
