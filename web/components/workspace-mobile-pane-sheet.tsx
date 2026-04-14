"use client";

import { ReactNode, useState } from "react";

import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { portalContainerContext } from "@/lib/portal-container";

type WorkspaceMobilePaneSheetProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string | null;
  children: ReactNode;
};

export function WorkspaceMobilePaneSheet({
  open,
  onOpenChange,
  title,
  description,
  children,
}: WorkspaceMobilePaneSheetProps) {
  const [portalContainer, setPortalContainer] = useState<HTMLElement | null>(null);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        ref={setPortalContainer}
        side="bottom"
        className="h-[88vh] rounded-t-2xl p-0 sm:max-w-none"
      >
        <SheetHeader className="border-b px-4 py-3 text-left">
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription className="truncate">
            {description ?? "Edit the selected configuration."}
          </SheetDescription>
        </SheetHeader>
        <portalContainerContext.Provider value={portalContainer}>
          <div className="min-h-0 flex-1 overflow-hidden">{children}</div>
        </portalContainerContext.Provider>
      </SheetContent>
    </Sheet>
  );
}
