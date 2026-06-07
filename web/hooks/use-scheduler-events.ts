import { useSetAtom } from "jotai";
import { useEffect } from "react";

import { SchedulerRunEvent, schedulerRunEventAtom } from "@/lib/atoms/domains/results";

function isSchedulerRunEvent(payload: unknown): payload is SchedulerRunEvent {
  return (
    typeof payload === "object" &&
    payload !== null &&
    "type" in payload &&
    typeof payload.type === "string" &&
    payload.type.startsWith("run.")
  );
}

export function useSchedulerEvents() {
  const setSchedulerRunEvent = useSetAtom(schedulerRunEventAtom);

  useEffect(() => {
    const source = new EventSource("/api/events");
    source.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as unknown;
        if (isSchedulerRunEvent(payload)) {
          setSchedulerRunEvent(payload);
        }
      } catch {
        return;
      }
    };

    return () => source.close();
  }, [setSchedulerRunEvent]);
}
