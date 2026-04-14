import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import {
  OnboardingDiscoveryResponse,
  OnboardingImportResponse,
  OnboardingPathSuggestionsResponse,
  OnboardingSessionState,
} from "@/lib/types";

export async function getOnboardingState(): Promise<OnboardingSessionState> {
  return fetchJSON<OnboardingSessionState>("/api/onboarding/state", {
    cache: "no-store",
  });
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
  return fetchJSONWithBody<OnboardingImportResponse>(
    "/api/onboarding/import",
    "POST",
    input
  );
}

export async function previewOnboardingDiscovery(input: {
  environment_name: string;
  type: string;
  values: Record<string, unknown>;
  database?: string;
}): Promise<OnboardingDiscoveryResponse> {
  return fetchJSONWithBody<OnboardingDiscoveryResponse>(
    "/api/onboarding/discovery",
    "POST",
    input
  );
}

export async function getOnboardingPathSuggestions(prefix?: string) {
  const search = new URLSearchParams();
  if (prefix?.trim()) {
    search.set("prefix", prefix.trim());
  }

  const query = search.toString();
  return fetchJSON<OnboardingPathSuggestionsResponse>(
    `/api/onboarding/path-suggestions${query ? `?${query}` : ""}`,
    { cache: "no-store" }
  );
}

export async function updateOnboardingState(
  state: OnboardingSessionState
): Promise<{ status: string }> {
  return fetchJSONWithBody<{ status: string }>("/api/onboarding/state", "PUT", state);
}
