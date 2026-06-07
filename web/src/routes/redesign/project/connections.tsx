import { createFileRoute } from "@tanstack/react-router";

import { RedesignProjectConnectionsPage } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/project/connections")({
  component: RedesignProjectConnectionsPage,
});
