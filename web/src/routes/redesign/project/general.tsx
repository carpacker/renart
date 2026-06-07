import { createFileRoute } from "@tanstack/react-router";

import { RedesignProjectGeneralPage } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/project/general")({
  component: RedesignProjectGeneralPage,
});
