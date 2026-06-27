import { createFileRoute } from "@tanstack/react-router";

import { RedesignNotebookLivePage } from "@/components/redesign/notebook-page";

export const Route = createFileRoute("/redesign/notebooks/$notebookId")({
  component: RedesignNotebookRoute,
});

function RedesignNotebookRoute() {
  const { notebookId } = Route.useParams();
  return <RedesignNotebookLivePage notebookId={notebookId} />;
}
