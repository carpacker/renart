import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/pipelines/$pipelineId/")({
  component: AppPipelineIndexRoute,
});

function AppPipelineIndexRoute() {
  const { pipelineId } = Route.useParams();
  return <Navigate to="/pipelines/$pipelineId/canvas" params={{ pipelineId }} />;
}
