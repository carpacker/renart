import { buildQueryString, fetchJSON } from "@/lib/api-core";

export type SlingDiscoveryStream = {
  name: string;
  schema?: string;
};

export type SlingDiscoveryResponse = {
  status: string;
  connection_name: string;
  pattern?: string;
  streams: SlingDiscoveryStream[];
  raw_output?: string;
  error?: string;
};

// discoverSlingStreams lists the objects/streams a bruin connection exposes
// (via `sling conns discover`) for source/target intellisense in the editor.
export async function discoverSlingStreams(options: {
  connection: string;
  pattern?: string;
  environment?: string;
  signal?: AbortSignal;
}) {
  return fetchJSON<SlingDiscoveryResponse>(
    `/api/sling/discover${buildQueryString({
      connection: options.connection,
      pattern: options.pattern,
      environment: options.environment,
    })}`,
    { cache: "no-store", signal: options.signal }
  );
}
