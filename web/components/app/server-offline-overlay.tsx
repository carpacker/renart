import { useAtomValue } from "jotai";
import { Loader2, WifiOff } from "lucide-react";

import { serverOnlineAtom } from "@/lib/atoms/domains/workspace";

// Full-screen overlay shown when the renart server becomes unreachable. The
// connection state is driven by the SSE stream in useWorkspaceSync; the overlay
// dismisses itself automatically once EventSource reconnects.
export function ServerOfflineOverlay() {
  const online = useAtomValue(serverOnlineAtom);
  if (online) {
    return null;
  }
  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-background/80 backdrop-blur-sm">
      <div className="mx-4 flex max-w-sm flex-col items-center gap-4 rounded-xl border bg-card p-6 text-center shadow-lg">
        <div className="flex size-12 items-center justify-center rounded-full bg-muted">
          <WifiOff className="size-6 text-muted-foreground" />
        </div>
        <div className="space-y-1">
          <h2 className="text-base font-semibold">Lost connection to the server</h2>
          <p className="text-sm text-muted-foreground">
            The renart server isn&apos;t responding. Your work is safe on disk. This will clear as soon as the
            connection is back.
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="size-3.5 animate-spin" />
          Reconnecting…
        </div>
      </div>
    </div>
  );
}
