import { createFileRoute, useLocation, useNavigate, useParams } from "@tanstack/react-router";

import {
  AppBuildPage,
  normalizeAppBuildSearch,
  appAssetViewPath,
  appBuildViewFromPath,
} from "@/components/app/build-page";

export const Route = createFileRoute("/_shell/pipelines/$pipelineId")({
  validateSearch: normalizeAppBuildSearch,
  component: AppPipelineLayoutRoute,
});

function AppPipelineLayoutRoute() {
  const { pipelineId } = Route.useParams();
  const allParams = useParams({ strict: false }) as { assetId?: string };
  const search = Route.useSearch();
  const navigate = useNavigate();
  const location = useLocation();
  const currentView = appBuildViewFromPath(location.pathname);
  const updateSearch = (nextSearch: Partial<typeof search>) => {
    navigate({
      to: location.pathname as never,
      search: { ...search, ...nextSearch } as never,
      replace: true,
    });
  };

  return (
    <AppBuildPage
      pipelineId={pipelineId}
      selectedAssetId={allParams.assetId}
      resultTab={search.result ?? "inspect"}
      editorMode={search.editor ?? "asset"}
      variant={search.variant ?? "default"}
      onResultTabChange={(result) => updateSearch({ result })}
      onVariantChange={(variant) => updateSearch({ variant })}
      onAssetSelect={(assetId) => navigate({
        to: appAssetViewPath(currentView),
        params: { pipelineId, assetId },
        search: { ...search, editor: "asset" },
      })}
    />
  );
}
