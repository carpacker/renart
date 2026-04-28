import { createFileRoute } from "@tanstack/react-router";

import { WorkspacePipelineConfigSectionPage } from "@/components/workspace-pipeline-config-dialog";

export const Route = createFileRoute("/_workspace/pipelines/$pipelineId/config/preview")({
  component: PipelineConfigPreviewRouteComponent,
});

function PipelineConfigPreviewRouteComponent() {
  return <WorkspacePipelineConfigSectionPage section="YAML Preview" />;
}
