import { createFileRoute } from "@tanstack/react-router";

import { RedesignNotebookPage } from "@/components/redesign/object-pages";

export const Route = createFileRoute("/redesign/notebooks/$notebookId")({
  component: RedesignNotebookRoute,
});

function RedesignNotebookRoute() {
  const { notebookId } = Route.useParams();
  return <RedesignNotebookPage notebookId={notebookId} />;
}
