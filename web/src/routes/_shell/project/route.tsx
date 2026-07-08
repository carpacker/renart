import { createFileRoute } from "@tanstack/react-router";

import { AppProjectSettingsShell } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/project")({
  component: AppProjectSettingsShell,
});
