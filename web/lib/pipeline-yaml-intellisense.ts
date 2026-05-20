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

export const PIPELINE_SCHEDULE_LANGUAGE = "renart-pipeline-schedule";

let scheduleLanguageRegistered = false;

export function registerPipelineScheduleLanguage(monaco: Monaco) {
  if (scheduleLanguageRegistered) {
    return;
  }

  monaco.languages.register({ id: PIPELINE_SCHEDULE_LANGUAGE });
  monaco.languages.setMonarchTokensProvider(PIPELINE_SCHEDULE_LANGUAGE, {
    tokenizer: {
      root: [
        [/@(?:daily|hourly)\b/, "schedule.preset"],
        [/@[\w-]*/, "schedule.invalid"],
        [/\*/, "schedule.wildcard"],
        [/\d+/, "schedule.number"],
        [/[,-/]/, "schedule.operator"],
        [/\s+/, "white"],
        [/./, "schedule.invalid"],
      ],
    },
  });

  scheduleLanguageRegistered = true;
}

export function getPipelineScheduleCompletionItems({
  monaco,
  range,
  value,
}: {
  monaco: Monaco;
  range: MonacoNS.IRange;
  value: string;
}): MonacoNS.languages.CompletionItem[] {
  const completions: MonacoNS.languages.CompletionItem[] = PIPELINE_SCHEDULE_COMPLETIONS.map((item) => ({
    label: item.label,
    kind: monaco.languages.CompletionItemKind.Value,
    detail: item.detail,
    filterText: item.label,
    insertText: item.insertText,
    range,
  }));

  const cronCompletion = completeCronSchedule(value);
  if (cronCompletion && !completions.some((item) => item.insertText === cronCompletion)) {
    completions.unshift({
      label: cronCompletion,
      kind: monaco.languages.CompletionItemKind.Value,
      detail: "Complete five-field cron expression.",
      filterText: value.trim(),
      insertText: cronCompletion,
      range,
    });
  }

  return completions;
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

function completeCronSchedule(value: string) {
  const normalized = value.trim();
  if (!normalized || normalized.startsWith("@")) {
    return null;
  }

  const fields = normalized.split(/\s+/).filter(Boolean);
  if (fields.length === 0 || fields.length >= 5) {
    return null;
  }
  if (!fields.every(isPartialCronField)) {
    return null;
  }

  return [...fields, ...Array.from({ length: 5 - fields.length }, () => "*")].join(" ");
}

function isPartialCronField(value: string) {
  return /^[\d*,/-]+$/.test(value);
}
