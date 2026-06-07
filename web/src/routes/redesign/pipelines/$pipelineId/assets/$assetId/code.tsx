import { createFileRoute } from "@tanstack/react-router";

import { RedesignBuildCodeView } from "@/components/redesign/build-page";

export const Route = createFileRoute("/redesign/pipelines/$pipelineId/assets/$assetId/code")({
  component: RedesignBuildCodeView,
});
