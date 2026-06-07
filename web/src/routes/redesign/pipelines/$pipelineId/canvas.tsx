import { createFileRoute } from "@tanstack/react-router";

import { RedesignBuildCanvasView } from "@/components/redesign/build-page";

export const Route = createFileRoute("/redesign/pipelines/$pipelineId/canvas")({
  component: RedesignBuildCanvasView,
});
