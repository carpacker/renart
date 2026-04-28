import { Outlet, createFileRoute } from "@tanstack/react-router";

import { WorkspacePipelineConfigDialogLayout } from "@/components/workspace-pipeline-config-dialog";

export const Route = createFileRoute("/_workspace/pipelines/$pipelineId/config")({
  component: PipelineConfigLayoutRouteComponent,
});

function PipelineConfigLayoutRouteComponent() {
  return (
    <WorkspacePipelineConfigDialogLayout>
      <Outlet />
    </WorkspacePipelineConfigDialogLayout>
  );
}
