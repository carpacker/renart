import { createFileRoute } from "@tanstack/react-router";

import { AppNotebookLivePage } from "@/components/app/notebook-page";

export const Route = createFileRoute("/_shell/notebooks/$notebookId")({
  component: AppNotebookRoute,
});

function AppNotebookRoute() {
  const { notebookId } = Route.useParams();
  return <AppNotebookLivePage notebookId={notebookId} />;
}
