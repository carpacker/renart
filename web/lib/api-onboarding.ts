import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import {
  OnboardingDiscoveryResponse,
  OnboardingImportResponse,
  OnboardingPathSuggestionsResponse,
  OnboardingSessionState,
  WorkspaceConnectionSecretChanges,
} from "@/lib/types";

let cachedOnboardingState: OnboardingSessionState | null = null;
let pendingOnboardingState: Promise<OnboardingSessionState> | null = null;

export async function getOnboardingState(options?: {
  cache?: boolean;
}): Promise<OnboardingSessionState> {
  if (options?.cache && cachedOnboardingState) {
    return cachedOnboardingState;
  }
  if (options?.cache && pendingOnboardingState) {
    return pendingOnboardingState;
  }

  const request = fetchJSON<OnboardingSessionState>("/api/onboarding/state", {
    cache: "no-store",
  });

  if (!options?.cache) {
    const state = await request;
    cachedOnboardingState = state;
    return state;
  }

  pendingOnboardingState = request
    .then((state) => {
      cachedOnboardingState = state;
      return state;
    })
    .finally(() => {
      pendingOnboardingState = null;
    });

  return pendingOnboardingState;
}

export async function importOnboardingDatabase(input: {
  connection_name: string;
  environment_name: string;
  pipeline_name: string;
  schema?: string;
  pattern?: string;
  tables?: string[];
  disable_columns?: boolean;
  create_if_missing?: boolean;
}): Promise<OnboardingImportResponse> {
  invalidateOnboardingStateCache();
  return fetchJSONWithBody<OnboardingImportResponse>("/api/onboarding/import", "POST", input);
}

export async function createDuckDBQuickstart(input: {
  environment_name?: string;
  pipeline_name?: string;
  connection_name?: string;
  database_path?: string;
  materialize?: boolean;
}): Promise<OnboardingImportResponse> {
  invalidateOnboardingStateCache();
  return fetchJSONWithBody<OnboardingImportResponse>("/api/onboarding/quickstart", "POST", input);
}

export async function previewOnboardingDiscovery(input: {
  environment_name: string;
  type: string;
  values: Record<string, unknown>;
  secret_changes?: WorkspaceConnectionSecretChanges;
  database?: string;
}): Promise<OnboardingDiscoveryResponse> {
  return fetchJSONWithBody<OnboardingDiscoveryResponse>("/api/onboarding/discovery", "POST", input);
}

export async function getOnboardingPathSuggestions(prefix?: string) {
  const search = new URLSearchParams();
  if (prefix?.trim()) {
    search.set("prefix", prefix.trim());
  }

  const query = search.toString();
  return fetchJSON<OnboardingPathSuggestionsResponse>(
    `/api/onboarding/path-suggestions${query ? `?${query}` : ""}`,
    { cache: "no-store" },
  );
}

export async function updateOnboardingState(
  state: OnboardingSessionState,
): Promise<{ status: string }> {
  cachedOnboardingState = state;
  pendingOnboardingState = null;
  return fetchJSONWithBody<{ status: string }>("/api/onboarding/state", "PUT", state);
}

function invalidateOnboardingStateCache() {
  cachedOnboardingState = null;
  pendingOnboardingState = null;
}
