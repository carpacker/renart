import { createFileRoute } from "@tanstack/react-router";

import { RedesignAccountSettingsShell } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/account")({
  component: RedesignAccountSettingsShell,
});
