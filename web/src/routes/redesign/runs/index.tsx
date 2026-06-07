import { createFileRoute } from "@tanstack/react-router";

import { RedesignRunsPage, normalizeRedesignRunsSearch } from "@/components/redesign/runs-page";

export const Route = createFileRoute("/redesign/runs/")({
  validateSearch: normalizeRedesignRunsSearch,
  component: RedesignRunsIndexRoute,
});

function RedesignRunsIndexRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return <RedesignRunsPage search={search} onSearchChange={(next) => navigate({ search: next, replace: true })} />;
}
