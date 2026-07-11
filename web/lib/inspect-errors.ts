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

	if (
		trimmed.includes(
			"Inspect only supports read-only single SELECT queries"
		)
	) {
		return trimmed;
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

export function extractMissingReferencedObjects(value: string | undefined): string[] {
  const trimmed = (value ?? "").trim();
  if (!trimmed) {
    return [];
  }

  const patterns = [
    /table with name ([a-zA-Z0-9_."]+) does not exist/gi,
    /relation ([a-zA-Z0-9_."]+) does not exist/gi,
    /no such table:?\s*([a-zA-Z0-9_."]+)/gi,
    /object ([a-zA-Z0-9_."]+) does not exist/gi,
  ];
  const result = new Set<string>();

  for (const pattern of patterns) {
    let match: RegExpExecArray | null;
    do {
      match = pattern.exec(trimmed);
      if (match?.[1]) {
        result.add(match[1].replaceAll('"', "").trim().toLowerCase());
      }
    } while (match);
  }

  return [...result];
}
