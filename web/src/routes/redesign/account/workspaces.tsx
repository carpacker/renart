import { createFileRoute } from "@tanstack/react-router";

import { RedesignAccountWorkspacesPage } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/account/workspaces")({
  component: RedesignAccountWorkspacesPage,
});
