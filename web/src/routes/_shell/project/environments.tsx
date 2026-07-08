import { createFileRoute } from "@tanstack/react-router";

import { AppProjectEnvironmentsPage } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/project/environments")({
  component: AppProjectEnvironmentsPage,
});
