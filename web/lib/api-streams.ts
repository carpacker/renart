import {
  getResponseErrorMessage,
  MaterializeStreamPayload,
} from "@/lib/api-core";

export async function readSSEStream(
  res: Response,
  handlers: {
    onChunk?: (chunk: string) => void;
    onDone?: (payload: MaterializeStreamPayload) => void;
  },
  endedMessage: string
) {
  if (!res.ok) {
    throw new Error(await getResponseErrorMessage(res));
  }

  if (!res.body) {
    throw new Error("Streaming response body is not available.");
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let donePayload: MaterializeStreamPayload | null = null;

  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });

    let delimiterIndex = buffer.indexOf("\n\n");
    while (delimiterIndex >= 0) {
      const rawEvent = buffer.slice(0, delimiterIndex);
      buffer = buffer.slice(delimiterIndex + 2);
      delimiterIndex = buffer.indexOf("\n\n");

      const parsed = parseSSEEvent(rawEvent);
      if (!parsed) {
        continue;
      }

      if (parsed.event === "output" && typeof parsed.data?.chunk === "string") {
        handlers.onChunk?.(parsed.data.chunk);
      }

      if (parsed.event === "done") {
        donePayload = parsed.data;
        handlers.onDone?.(parsed.data);
      }
    }

    if (done) {
      break;
    }
  }

  if (!donePayload) {
    throw new Error(endedMessage);
  }

  return donePayload;
}

export async function streamMaterialization(
  path: string,
  handlers: {
    onChunk?: (chunk: string) => void;
    onDone?: (payload: MaterializeStreamPayload) => void;
  },
  endedMessage: string
) {
  const res = await fetch(path, {
    method: "POST",
    headers: {
      Accept: "text/event-stream",
    },
  });

  return readSSEStream(res, handlers, endedMessage);
}

function parseSSEEvent(rawEvent: string): {
  event: string;
  data: MaterializeStreamPayload;
} | null {
  const lines = rawEvent.split(/\r?\n/);
  let event = "message";
  const dataLines: string[] = [];

  for (const line of lines) {
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
      continue;
    }

    if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trim());
    }
  }

  if (dataLines.length === 0) {
    return null;
  }

  return {
    event,
    data: JSON.parse(dataLines.join("\n")) as MaterializeStreamPayload,
  };
}
