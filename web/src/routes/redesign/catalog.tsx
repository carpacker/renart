import { createFileRoute } from "@tanstack/react-router";

import { RedesignCatalogPage, normalizeRedesignCatalogSearch } from "@/components/redesign/catalog-page";

export const Route = createFileRoute("/redesign/catalog")({
  validateSearch: normalizeRedesignCatalogSearch,
  component: RedesignCatalogRoute,
});

function RedesignCatalogRoute() {
  const search = Route.useSearch();
  return <RedesignCatalogPage selectedAssetId={search.asset} />;
}
