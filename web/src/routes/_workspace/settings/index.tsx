import { createFileRoute, useNavigate } from "@tanstack/react-router";

import { WorkspaceEnvironmentsRoutePage } from "./environments";

export const Route = createFileRoute("/_workspace/settings/")({
  component: WorkspaceSettingsIndexRouteComponent,
});

function WorkspaceSettingsIndexRouteComponent() {
  const navigate = useNavigate();

  return (
    <WorkspaceEnvironmentsRoutePage
      onSearchChange={(next) =>
        navigate({
          to: "/settings/environments",
          search: {
            environment: next.environment,
            mode: next.mode,
          },
          replace: true,
        })
      }
    />
  );
}
