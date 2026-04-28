import { createFileRoute } from "@tanstack/react-router";

import { WorkspacePipelineConfigSectionPage } from "@/components/workspace-pipeline-config-dialog";

export const Route = createFileRoute("/_workspace/pipelines/$pipelineId/config/general")({
  component: PipelineConfigGeneralRouteComponent,
});

function PipelineConfigGeneralRouteComponent() {
  return <WorkspacePipelineConfigSectionPage section="General" />;
}
