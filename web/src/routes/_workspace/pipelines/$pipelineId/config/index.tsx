import { createFileRoute } from "@tanstack/react-router";

import { WorkspacePipelineConfigSectionsPage } from "@/components/workspace-pipeline-config-dialog";

export const Route = createFileRoute("/_workspace/pipelines/$pipelineId/config/")({
  component: PipelineConfigIndexRouteComponent,
});

function PipelineConfigIndexRouteComponent() {
  return <WorkspacePipelineConfigSectionsPage />;
}
