import { createFileRoute } from "@tanstack/react-router";

import { RedesignSchedulesPage } from "@/components/redesign/schedules-page";

export const Route = createFileRoute("/redesign/schedules")({
  component: RedesignSchedulesPage,
});
