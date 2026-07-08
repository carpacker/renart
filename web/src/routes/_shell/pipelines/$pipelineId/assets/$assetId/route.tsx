import { Outlet, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/pipelines/$pipelineId/assets/$assetId")({
  component: Outlet,
});
