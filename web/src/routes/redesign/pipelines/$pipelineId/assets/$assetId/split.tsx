import { createFileRoute } from "@tanstack/react-router";

import { RedesignBuildSplitView } from "@/components/redesign/build-page";

export const Route = createFileRoute("/redesign/pipelines/$pipelineId/assets/$assetId/split")({
  component: RedesignBuildSplitView,
});
