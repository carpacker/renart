import { createFileRoute } from "@tanstack/react-router";

import { AppCatalogPage, normalizeAppCatalogSearch } from "@/components/app/catalog-page";

export const Route = createFileRoute("/_shell/catalog")({
  validateSearch: normalizeAppCatalogSearch,
  component: AppCatalogRoute,
});

function AppCatalogRoute() {
  const search = Route.useSearch();
  return <AppCatalogPage selectedAssetId={search.asset} />;
}
