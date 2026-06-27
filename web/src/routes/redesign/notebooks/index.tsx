import { createFileRoute } from "@tanstack/react-router";

import { RedesignNotebooksIndexPage } from "@/components/redesign/notebook-page";

export const Route = createFileRoute("/redesign/notebooks/")({
  component: RedesignNotebooksIndexPage,
});
