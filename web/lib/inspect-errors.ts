export function extractInspectErrorText(rawOutput: string | undefined): string {
  const trimmed = (rawOutput ?? "").trim();
  if (!trimmed) {
    return "";
  }

  try {
    const parsed = JSON.parse(trimmed) as {
      error?: unknown;
      message?: unknown;
    };

    if (typeof parsed.error === "string" && parsed.error.trim()) {
      return parsed.error.trim();
    }

    if (typeof parsed.message === "string" && parsed.message.trim()) {
      return parsed.message.trim();
    }
  } catch {
    return trimmed;
  }

  return trimmed;
}

export function normalizeInspectErrorMessage(value: string | undefined): string {
  const trimmed = (value ?? "").trim();
  if (!trimmed) {
    return "";
  }

  if (!trimmed.startsWith("Error:")) {
    return trimmed;
  }

  const remainder = trimmed.slice("Error:".length).trim();
  if (!remainder.startsWith("{")) {
    return remainder;
  }

  try {
    const parsed = JSON.parse(remainder) as {
      raw_output?: unknown;
      error?: unknown;
      message?: unknown;
    };

    if (typeof parsed.raw_output === "string") {
      const extracted = extractInspectErrorText(parsed.raw_output);
      if (extracted) {
        return extracted;
      }
    }

    if (typeof parsed.message === "string" && parsed.message.trim()) {
      return parsed.message.trim();
    }

    if (typeof parsed.error === "string" && parsed.error.trim()) {
      return parsed.error.trim();
    }
  } catch {
    return remainder;
  }

  return remainder;
}
