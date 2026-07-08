import { createFileRoute } from "@tanstack/react-router";

import { AppBuildCodeView } from "@/components/app/build-page";

export const Route = createFileRoute("/_shell/pipelines/$pipelineId/assets/$assetId/code")({
  component: AppBuildCodeView,
});
