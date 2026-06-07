import { createFileRoute } from "@tanstack/react-router";

import { RedesignCatalogPage } from "@/components/redesign/catalog-page";

export const Route = createFileRoute("/redesign/catalog")({
  component: RedesignCatalogPage,
});
