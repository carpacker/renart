import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/redesign/pipelines/$pipelineId/")({
  component: RedesignPipelineIndexRoute,
});

function RedesignPipelineIndexRoute() {
  const { pipelineId } = Route.useParams();
  return <Navigate to="/redesign/pipelines/$pipelineId/canvas" params={{ pipelineId }} />;
}
