import { Navigate, createFileRoute } from "@tanstack/react-router";
import { useAtomValue } from "jotai";

import { Spinner } from "@/components/ui/spinner";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";

export const Route = createFileRoute("/_shell/")({
  component: AppBuildIndexRoute,
});

// The home route waits for the workspace, then lands on the first pipeline's
// canvas — or the welcome/onboarding screen when the workspace has none.
function AppBuildIndexRoute() {
  const workspace = useAtomValue(workspaceAtom);

  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="size-5 text-muted-foreground" />
      </div>
    );
  }

  if (workspace.pipelines.length === 0) {
    return <Navigate to="/welcome" />;
  }

  return (
    <Navigate
      to="/pipelines/$pipelineId/canvas"
      params={{ pipelineId: workspace.pipelines[0].id }}
    />
  );
}
