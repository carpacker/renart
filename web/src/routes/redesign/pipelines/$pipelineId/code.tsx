import { createFileRoute } from "@tanstack/react-router";

import { RedesignBuildCodeView } from "@/components/redesign/build-page";

export const Route = createFileRoute("/redesign/pipelines/$pipelineId/code")({
  component: RedesignBuildCodeView,
});
