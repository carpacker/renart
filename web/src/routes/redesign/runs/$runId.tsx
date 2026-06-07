import { createFileRoute } from "@tanstack/react-router";

import { RedesignRunsPage, normalizeRedesignRunsSearch } from "@/components/redesign/runs-page";

export const Route = createFileRoute("/redesign/runs/$runId")({
  validateSearch: normalizeRedesignRunsSearch,
  component: RedesignRunDetailRoute,
});

function RedesignRunDetailRoute() {
  const { runId } = Route.useParams();
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return <RedesignRunsPage selectedRunId={runId} search={search} onSearchChange={(next) => navigate({ search: next, replace: true })} />;
}
