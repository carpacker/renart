"use client";

import { AlertTriangle } from "lucide-react";

import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";

export function InspectWarningCard({
  message,
  testId,
}: {
  message: string;
  testId?: string;
}) {
  return (
    <Alert
      className="max-w-2xl bg-background/96 border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-50"
      data-testid={testId}
    >
      <AlertTriangle className="text-amber-500" />
      <AlertTitle>Preview Failed</AlertTitle>
      <AlertDescription className="whitespace-pre-wrap font-[monospace] text-xs leading-5">
        {message}
      </AlertDescription>
    </Alert>
  );
}
