import { useCallback, useEffect, useState } from "react";

import {
  checkoutSourceControlBranch,
  commitSourceControlChanges,
  getSourceControlBranches,
  getSourceControlDiff,
  getSourceControlStatus,
  stageSourceControlPaths,
  unstageSourceControlPaths,
} from "@/lib/api";
import type { SourceControlChange, SourceControlDiff, SourceControlRepository } from "@/lib/types";

export function useSourceControl() {
  const [repository, setRepository] = useState<SourceControlRepository | null>(null);
  const [branches, setBranches] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [diffLoading, setDiffLoading] = useState(false);
  const [diff, setDiff] = useState<SourceControlDiff | null>(null);
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [statusResponse, branchesResponse] = await Promise.all([
        getSourceControlStatus(),
        getSourceControlBranches(),
      ]);
      if (statusResponse.status === "ok") {
        setRepository(statusResponse.repository);
      }
      if (branchesResponse.status === "ok") {
        setBranches(branchesResponse.branches ?? []);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load source control status");
    } finally {
      setLoading(false);
    }
  }, []);

  const run = useCallback(async (action: () => Promise<{ status: "ok" | "error"; repository?: SourceControlRepository }>) => {
    setBusy(true);
    setError("");
    try {
      const response = await action();
      if (response.status === "ok" && response.repository) {
        setRepository(response.repository);
      }
      return response;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Source control operation failed");
      throw err;
    } finally {
      setBusy(false);
    }
  }, [refresh]);

  const updateChanges = useCallback((paths: string[], update: (change: SourceControlChange) => SourceControlChange | null) => {
    const wanted = new Set(paths);
    setRepository((current) => {
      if (!current) return current;
      const changes = current.changes
        .map((change) => wanted.has(change.path) ? update(change) : change)
        .filter((change): change is SourceControlChange => Boolean(change));
      return { ...current, clean: changes.length === 0, changes };
    });
  }, []);

  const stage = useCallback(async (paths: string[]) => {
    await run(() => stageSourceControlPaths(paths));
    updateChanges(paths, (change) => ({
      ...change,
      staged: true,
      staged_status: change.worktree_status === "?" ? "A" : change.worktree_status || change.staged_status || "M",
      worktree_status: "",
    }));
  }, [run, updateChanges]);

  const unstage = useCallback(async (paths: string[]) => {
    await run(() => unstageSourceControlPaths(paths));
    updateChanges(paths, (change) => ({
      ...change,
      staged: false,
      staged_status: "",
      worktree_status: change.worktree_status || (change.staged_status === "A" ? "?" : change.staged_status || "M"),
    }));
  }, [run, updateChanges]);

  const checkout = useCallback(async (branch: string) => {
    await run(() => checkoutSourceControlBranch(branch));
    setRepository((current) => current ? { ...current, branch } : current);
  }, [run]);

  const commit = useCallback(async (message: string) => {
    await run(() => commitSourceControlChanges(message));
    setRepository((current) => {
      if (!current) return current;
      const changes = current.changes.filter((change) => !change.staged);
      return { ...current, clean: changes.length === 0, changes };
    });
  }, [run]);

  const loadDiff = useCallback(async (path: string, staged: boolean) => {
    setDiffLoading(true);
    setError("");
    try {
      const response = await getSourceControlDiff(path, staged);
      if (response.status === "ok") {
        setDiff(response.diff);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load diff");
    } finally {
      setDiffLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return {
    repository,
    branches,
    loading,
    busy,
    diffLoading,
    diff,
    error,
    refresh,
    loadDiff,
    stage,
    unstage,
    checkout,
    commit,
  };
}
