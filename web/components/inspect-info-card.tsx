"use client";

import { Info } from "lucide-react";
import { ReactNode } from "react";

import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";

/**
 * A neutral, informational counterpart to InspectWarningCard: used when there is
 * simply nothing to preview (e.g. a load asset that writes to a local file), so
 * the message reads as an explanation rather than a failure. Deliberately gray —
 * no amber/red — to avoid signalling an error.
 */
export function InspectInfoCard({
  message,
  testId,
  actions,
}: {
  message: string;
  testId?: string;
  actions?: ReactNode;
}) {
  return (
    <Alert
      className="max-w-2xl border-border bg-muted/40 text-muted-foreground"
      data-testid={testId}
    >
      <Info className="text-muted-foreground" />
      <AlertTitle className="text-foreground">Nothing to preview</AlertTitle>
      <AlertDescription className="whitespace-pre-wrap text-xs leading-5 text-muted-foreground">
        {message}
      </AlertDescription>
      {actions ? <div className="mt-3">{actions}</div> : null}
    </Alert>
  );
}
