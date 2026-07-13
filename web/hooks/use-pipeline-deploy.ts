import { useAtomValue } from "jotai";
import { useCallback, useEffect, useState } from "react";

import { deployPipeline, getDeployStatus, type DeployStatus } from "@/lib/api-deploy";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";

export type PipelineDeployState = {
  status: DeployStatus | null;
  deploying: boolean;
  deploy: () => Promise<void>;
  refresh: () => Promise<void>;
  driftedFileCount: number;
};

// usePipelineDeploy tracks drift between the working tree and the latest
// deployed snapshot, refreshing whenever the workspace revision changes
// (i.e. after any save).
export function usePipelineDeploy(pipelineId: string | undefined): PipelineDeployState {
  const workspace = useAtomValue(workspaceAtom);
  const [status, setStatus] = useState<DeployStatus | null>(null);
  const [deploying, setDeploying] = useState(false);

  const refresh = useCallback(async () => {
    if (!pipelineId) {
      setStatus(null);
      return;
    }
    try {
      setStatus(await getDeployStatus(pipelineId));
    } catch {
      setStatus(null);
    }
  }, [pipelineId]);

  useEffect(() => {
    void refresh();
  }, [refresh, workspace?.revision]);

  const deploy = useCallback(async () => {
    if (!pipelineId || deploying) return;
    setDeploying(true);
    try {
      await deployPipeline(pipelineId);
      await refresh();
    } finally {
      setDeploying(false);
    }
  }, [deploying, pipelineId, refresh]);

  const driftedFileCount =
    (status?.changed_files?.length ?? 0) +
    (status?.added_files?.length ?? 0) +
    (status?.removed_files?.length ?? 0);

  return { status, deploying, deploy, refresh, driftedFileCount };
}
