import { createFileRoute, useLocation, useNavigate, useParams } from "@tanstack/react-router";

import {
  RedesignBuildPage,
  normalizeRedesignBuildSearch,
  redesignAssetViewPath,
  redesignBuildViewFromPath,
} from "@/components/redesign/build-page";

export const Route = createFileRoute("/redesign/pipelines/$pipelineId")({
  validateSearch: normalizeRedesignBuildSearch,
  component: RedesignPipelineLayoutRoute,
});

function RedesignPipelineLayoutRoute() {
  const { pipelineId } = Route.useParams();
  const allParams = useParams({ strict: false }) as { assetId?: string };
  const search = Route.useSearch();
  const navigate = useNavigate();
  const location = useLocation();
  const currentView = redesignBuildViewFromPath(location.pathname);
  const updateSearch = (nextSearch: Partial<typeof search>) => {
    navigate({
      to: location.pathname as never,
      search: { ...search, ...nextSearch } as never,
      replace: true,
    });
  };

  return (
    <RedesignBuildPage
      pipelineId={pipelineId}
      selectedAssetId={allParams.assetId}
      resultTab={search.result ?? "inspect"}
      editorMode={search.editor ?? "asset"}
      variant={search.variant ?? "default"}
      onResultTabChange={(result) => updateSearch({ result })}
      onEditorModeChange={(editor) => updateSearch({ editor })}
      onVariantChange={(variant) => updateSearch({ variant })}
      onAssetSelect={(assetId) => navigate({
        to: redesignAssetViewPath(currentView),
        params: { pipelineId, assetId },
        search: { ...search, editor: "asset" },
      })}
    />
  );
}
