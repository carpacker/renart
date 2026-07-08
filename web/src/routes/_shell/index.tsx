import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/")({
  component: AppBuildIndexRoute,
});

function AppBuildIndexRoute() {
  return <Navigate to="/pipelines/$pipelineId/canvas" params={{ pipelineId: "simple" }} />;
}
