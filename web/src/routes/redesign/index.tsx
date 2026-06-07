import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/redesign/")({
  component: RedesignBuildIndexRoute,
});

function RedesignBuildIndexRoute() {
  return <Navigate to="/redesign/pipelines/$pipelineId/canvas" params={{ pipelineId: "simple" }} />;
}
