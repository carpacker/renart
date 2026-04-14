import { buildQueryString, fetchJSON } from "@/lib/api-core";
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
    { cache: "no-store" }
  );
}
