import { createFileRoute } from "@tanstack/react-router";

import { AppRunsPage, normalizeAppRunsSearch } from "@/components/app/runs-page";

export const Route = createFileRoute("/_shell/runs/")({
  validateSearch: normalizeAppRunsSearch,
  component: AppRunsIndexRoute,
});

function AppRunsIndexRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <AppRunsPage
      search={search}
      onSearchChange={(next) => navigate({ search: next, replace: true })}
    />
  );
}
