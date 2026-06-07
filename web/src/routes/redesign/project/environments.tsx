import { createFileRoute } from "@tanstack/react-router";

import { RedesignProjectEnvironmentsPage } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/project/environments")({
  component: RedesignProjectEnvironmentsPage,
});
