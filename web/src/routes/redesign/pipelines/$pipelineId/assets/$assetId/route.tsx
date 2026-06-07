import { Outlet, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/redesign/pipelines/$pipelineId/assets/$assetId")({
  component: Outlet,
});
