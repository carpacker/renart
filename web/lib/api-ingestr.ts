import { buildQueryString, fetchJSON } from "@/lib/api-core";
import type { OpenAPISuggestionsResult } from "@/lib/generated/api-types";
import { IngestrSuggestionsResponse } from "@/lib/types";

export async function getIngestrSuggestions(options: {
  connection: string;
  prefix?: string;
  environment?: string;
}) {
  return fetchJSON<IngestrSuggestionsResponse>(
    `/api/ingestr/suggestions${buildQueryString({
      connection: options.connection,
      prefix: options.prefix,
      environment: options.environment,
    })}`,
    { cache: "no-store" },
  );
}

// Powers the API-asset editor's intellisense for `request.url` (spec paths) and
// `response.records_path` (record locations in the selected endpoint's response
// schema). openapiUrl is required; requestUrl/method narrow the records paths.
export async function getOpenAPISuggestions(options: {
  openapiUrl: string;
  requestUrl?: string;
  method?: string;
}) {
  return fetchJSON<OpenAPISuggestionsResult>(
    `/api/api-assets/openapi-suggestions${buildQueryString({
      openapi_url: options.openapiUrl,
      request_url: options.requestUrl,
      method: options.method,
    })}`,
    { cache: "no-store" },
  );
}
