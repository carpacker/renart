import { createFileRoute } from "@tanstack/react-router";

import { AppNotebooksIndexPage } from "@/components/app/notebook-page";

export const Route = createFileRoute("/_shell/notebooks/")({
  component: AppNotebooksIndexPage,
});
