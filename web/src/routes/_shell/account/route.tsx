import { createFileRoute } from "@tanstack/react-router";

import { AppAccountSettingsShell } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/account")({
  component: AppAccountSettingsShell,
});
