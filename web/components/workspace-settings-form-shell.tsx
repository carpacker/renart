"use client";

import { AlertCircle, RefreshCcw } from "lucide-react";
import { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

export function WorkspaceSettingsFormShell({
  title,
  configPath,
  loading,
  busy,
  parseError,
  statusMessage,
  statusTone,
  onReload,
  showHeader = true,
  showConfigPath = true,
  showReload = true,
  useCard = true,
  children,
}: {
  title: string;
  configPath?: string;
  loading: boolean;
  busy: boolean;
  parseError?: string;
  statusMessage?: string | null;
  statusTone?: "error" | "success" | null;
  onReload: () => void;
  showHeader?: boolean;
  showConfigPath?: boolean;
  showReload?: boolean;
  useCard?: boolean;
  children: ReactNode;
}) {
  const content = (
    <>
      {showHeader ? (
        <div className={useCard ? "border-b px-6 py-5" : "pb-4"}>
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <div className="text-lg font-semibold">{title}</div>
              {showConfigPath && configPath ? (
                <p className="mt-1 text-xs text-muted-foreground">{configPath}</p>
              ) : null}
            </div>

            {showReload ? (
              <Button
                size="sm"
                type="button"
                variant="outline"
                disabled={loading || busy}
                onClick={onReload}
              >
                <RefreshCcw className="mr-1 size-3" />
                Reload
              </Button>
            ) : null}
          </div>

          {statusMessage ? (
            <div
              className={`mt-4 rounded-md border px-3 py-2 text-sm ${
                statusTone === "error"
                  ? "border-destructive/40 bg-destructive/10 text-destructive"
                  : "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
              }`}
            >
              {statusMessage}
            </div>
          ) : null}

          {parseError ? (
            <div className="mt-4 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm text-amber-900 dark:text-amber-100">
              <div className="flex items-center gap-2 font-medium">
                <AlertCircle className="size-4" />
                Workspace config could not be parsed
              </div>
              <div className="mt-1 whitespace-pre-wrap text-xs sm:text-sm">{parseError}</div>
            </div>
          ) : null}
        </div>
      ) : null}

      <div className={useCard ? "p-6" : showHeader ? "pt-2" : ""}>{children}</div>
    </>
  );

  if (!useCard) {
    return content;
  }

  return <Card>{content}</Card>;
}

export function WorkspaceSettingsNotFoundCard({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <Card>
      <CardContent className="px-6 py-6">
        <h2 className="text-lg font-semibold">{title}</h2>
        <p className="mt-2 text-sm text-muted-foreground">{description}</p>
      </CardContent>
    </Card>
  );
}
