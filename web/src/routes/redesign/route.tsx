import { createFileRoute } from "@tanstack/react-router";

import { RedesignShell } from "@/components/redesign/redesign-shell";

export const Route = createFileRoute("/redesign")({
  component: RedesignShell,
});
