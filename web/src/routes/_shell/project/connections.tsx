import { createFileRoute } from "@tanstack/react-router";

import { AppProjectConnectionsPage } from "@/components/app/settings-pages";

type ProjectConnectionsSearch = {
  environment?: string;
  connection?: string;
};

function normalizeProjectConnectionsSearch(
  search: Record<string, unknown>,
): ProjectConnectionsSearch {
  return {
    environment: typeof search.environment === "string" ? search.environment : undefined,
    connection: typeof search.connection === "string" ? search.connection : undefined,
  };
}

export const Route = createFileRoute("/_shell/project/connections")({
  validateSearch: normalizeProjectConnectionsSearch,
  component: AppProjectConnectionsRoute,
});

function AppProjectConnectionsRoute() {
  const search = Route.useSearch();
  return (
    <AppProjectConnectionsPage
      selectedEnvironment={search.environment}
      selectedConnection={search.connection}
    />
  );
}
