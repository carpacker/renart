import { Outlet, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_workspace/pipelines/$pipelineId")({
  component: PipelineRouteComponent,
});

function PipelineRouteComponent() {
  return <Outlet />;
}
