import { createFileRoute } from "@tanstack/react-router";

import { AppAccountWorkspacesPage } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/account/workspaces")({
  component: AppAccountWorkspacesPage,
});
