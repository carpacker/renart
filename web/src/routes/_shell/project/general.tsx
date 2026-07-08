import { createFileRoute } from "@tanstack/react-router";

import { AppProjectGeneralPage } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/project/general")({
  component: AppProjectGeneralPage,
});
