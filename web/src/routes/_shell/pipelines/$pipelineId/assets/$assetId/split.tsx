import { createFileRoute } from "@tanstack/react-router";

import { AppBuildSplitView } from "@/components/app/build-page";

export const Route = createFileRoute("/_shell/pipelines/$pipelineId/assets/$assetId/split")({
  component: AppBuildSplitView,
});
