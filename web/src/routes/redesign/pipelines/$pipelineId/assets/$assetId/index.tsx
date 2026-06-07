import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/redesign/pipelines/$pipelineId/assets/$assetId/")({
  component: RedesignAssetIndexRoute,
});

function RedesignAssetIndexRoute() {
  const { pipelineId, assetId } = Route.useParams();
  return <Navigate to="/redesign/pipelines/$pipelineId/assets/$assetId/canvas" params={{ pipelineId, assetId }} />;
}
