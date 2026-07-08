import { createFileRoute } from "@tanstack/react-router";

import { AppBuildCanvasView } from "@/components/app/build-page";

export const Route = createFileRoute("/_shell/pipelines/$pipelineId/canvas")({
  component: AppBuildCanvasView,
});
