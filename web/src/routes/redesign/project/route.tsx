import { createFileRoute } from "@tanstack/react-router";

import { RedesignProjectSettingsShell } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/project")({
  component: RedesignProjectSettingsShell,
});
