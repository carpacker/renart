import type { Monaco } from "@monaco-editor/react";
import type * as MonacoNS from "monaco-editor";

export const PIPELINE_SCHEDULE_COMPLETIONS = [
  {
    label: "@daily",
    detail: "Runs once per day.",
    insertText: "@daily",
  },
  {
    label: "@hourly",
    detail: "Runs every hour.",
    insertText: "@hourly",
  },
  {
    label: "* * * * *",
    detail: "Custom cron expression.",
    insertText: "* * * * *",
  },
] as const;

export function getPipelineScheduleCompletionItems({
  monaco,
  range,
}: {
  monaco: Monaco;
  range: MonacoNS.IRange;
}): MonacoNS.languages.CompletionItem[] {
  return PIPELINE_SCHEDULE_COMPLETIONS.map((item) => ({
    label: item.label,
    kind: monaco.languages.CompletionItemKind.Value,
    detail: item.detail,
    filterText: item.label,
    insertText: item.insertText,
    range,
  }));
}

export function isValidPipelineSchedule(value: string) {
  const normalized = value.trim();
  if (!normalized) {
    return true;
  }
  if (normalized === "@daily" || normalized === "@hourly") {
    return true;
  }
  return normalized.split(/\s+/).length === 5;
}
