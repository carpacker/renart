import { useAtomValue } from "jotai";

import { selectedEnvironmentAtom, workspaceAtom } from "@/lib/atoms/domains/workspace";
import type { EnvironmentPolicy } from "@/lib/generated/api-types";

// useSelectedEnvironmentPolicy returns the execution policy for the
// currently selected environment (from .renart/environments.yml). This
// powers UI mirroring only — enforcement lives in the backend run-dispatch
// chokepoint.
export function useSelectedEnvironmentPolicy(): EnvironmentPolicy | undefined {
  const workspace = useAtomValue(workspaceAtom);
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  return workspace?.environment_policies?.[selectedEnvironment || "default"];
}
