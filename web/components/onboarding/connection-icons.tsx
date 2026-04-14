import { Database } from "lucide-react";

import { resolveAssetIcon } from "@/components/asset-type-icon";

export function OnboardingConnectionIcon({ type }: { type: string }) {
  const resolved = resolveAssetIcon(undefined, type, undefined, 22);
  return resolved?.icon ?? <Database className="size-5 text-muted-foreground" />;
}
