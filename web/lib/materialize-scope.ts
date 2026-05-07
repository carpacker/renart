export type MaterializeScope =
  | "asset"
  | "asset_with_upstreams"
  | "asset_with_downstreams"
  | "asset_with_upstreams_and_downstreams";

export const DEFAULT_MATERIALIZE_SCOPE: MaterializeScope = "asset";

export function labelForMaterializeScope(scope: MaterializeScope) {
  switch (scope) {
    case "asset":
      return "Materialize asset";
    case "asset_with_upstreams":
      return "Materialize with upstreams";
    case "asset_with_downstreams":
      return "Materialize with downstreams";
    case "asset_with_upstreams_and_downstreams":
      return "Materialize with upstreams and downstreams";
  }
}
