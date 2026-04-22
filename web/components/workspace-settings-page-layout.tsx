"use client";

import { ReactNode } from "react";

export function WorkspaceSettingsPageLayout({
  title,
  description,
  metadata,
  actions,
  children,
}: {
  title: string;
  description?: string;
  metadata?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="h-full min-h-0 overflow-auto bg-muted/10">
      <div className="mx-auto flex min-h-full w-full max-w-6xl flex-col gap-6 p-6 sm:p-8">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
            {description ? <p className="mt-2 text-sm text-muted-foreground">{description}</p> : null}
            {metadata ? <div className="mt-3">{metadata}</div> : null}
          </div>

          {actions ? <div className="flex shrink-0 flex-wrap gap-2">{actions}</div> : null}
        </div>

        <div className="min-h-0 flex-1 pb-2">{children}</div>
      </div>
    </div>
  );
}
