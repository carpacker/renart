import { createFileRoute } from "@tanstack/react-router";

import { AppProjectConnectionsPage } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/project/connections")({
  component: AppProjectConnectionsPage,
});
