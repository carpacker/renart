import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute(
  "/_workspace/settings/environments/$environmentId/"
)({
  component: EnvironmentHubSelectionRouteComponent,
});

function EnvironmentHubSelectionRouteComponent() {
  return null;
}
