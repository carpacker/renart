import { Navigate, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/pipelines/$pipelineId/assets/$assetId/")({
  component: AppAssetIndexRoute,
});

function AppAssetIndexRoute() {
  const { pipelineId, assetId } = Route.useParams();
  return (
    <Navigate to="/pipelines/$pipelineId/assets/$assetId/split" params={{ pipelineId, assetId }} />
  );
}
