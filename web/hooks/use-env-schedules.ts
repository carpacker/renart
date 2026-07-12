import { useAtomValue } from "jotai";
import { useCallback, useEffect, useState } from "react";

import {
  archiveEnvSchedule,
  getEnvSchedules,
  setEnvScheduleStatus,
  upsertEnvSchedule,
  type EnvSchedule,
  type UpsertEnvScheduleInput,
} from "@/lib/api-env-schedules";
import { schedulerRunEventAtom } from "@/lib/atoms/domains/results";

export function envScheduleKey(schedule: Pick<EnvSchedule, "pipeline_uuid" | "environment">) {
  return `${schedule.pipeline_uuid}::${schedule.environment}`;
}

// useEnvSchedules manages the per-(pipeline, environment) schedule rows.
export function useEnvSchedules() {
  const runEvent = useAtomValue(schedulerRunEventAtom);
  const [schedules, setSchedules] = useState<EnvSchedule[]>([]);
  const [archived, setArchived] = useState<EnvSchedule[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const response = await getEnvSchedules();
      setSchedules(response.schedules ?? []);
      setArchived(response.archived ?? []);
    } catch {
      // keep previous state
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Run lifecycle events update last-run displays.
  useEffect(() => {
    if (runEvent?.type === "run.finished" || runEvent?.type === "run.started") {
      void refresh();
    }
  }, [refresh, runEvent]);

  const withBusy = useCallback(
    async (key: string, action: () => Promise<unknown>) => {
      setBusyKey(key);
      try {
        await action();
        await refresh();
      } finally {
        setBusyKey(null);
      }
    },
    [refresh],
  );

  const upsert = useCallback(
    async (
      schedule: Pick<EnvSchedule, "pipeline_uuid" | "environment"> & { pipeline_id: string },
      input: UpsertEnvScheduleInput,
    ) => {
      await withBusy(envScheduleKey(schedule), () =>
        upsertEnvSchedule(schedule.pipeline_id, schedule.environment, input),
      );
    },
    [withBusy],
  );

  const setStatus = useCallback(
    async (schedule: EnvSchedule, status: "active" | "paused") => {
      if (!schedule.pipeline_id) return;
      await withBusy(envScheduleKey(schedule), () =>
        setEnvScheduleStatus(schedule.pipeline_id as string, schedule.environment, status),
      );
    },
    [withBusy],
  );

  const archive = useCallback(
    async (schedule: EnvSchedule) => {
      if (!schedule.pipeline_id) return;
      await withBusy(envScheduleKey(schedule), () =>
        archiveEnvSchedule(schedule.pipeline_id as string, schedule.environment),
      );
    },
    [withBusy],
  );

  return { schedules, archived, loading, busyKey, refresh, upsert, setStatus, archive };
}
