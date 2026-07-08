import { createFileRoute } from "@tanstack/react-router";

import { AppShell } from "@/components/app/app-shell";

export const Route = createFileRoute("/_shell")({
  component: AppShell,
});
