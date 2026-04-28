import { createFileRoute } from "@tanstack/react-router";

import { WorkspacePipelineConfigSectionPage } from "@/components/workspace-pipeline-config-dialog";

export const Route = createFileRoute("/_workspace/pipelines/$pipelineId/config/variables")({
  component: PipelineConfigVariablesRouteComponent,
});

function PipelineConfigVariablesRouteComponent() {
  return <WorkspacePipelineConfigSectionPage section="Variables" />;
}
