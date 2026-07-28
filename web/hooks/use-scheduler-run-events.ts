import { useAtomValue } from "jotai";
import { useEffect, useRef } from "react";

import { schedulerRunEventsAtom, type SchedulerRunEvent } from "@/lib/atoms/domains/results";

export function useSchedulerRunEvents(onEvent: (event: SchedulerRunEvent) => void) {
  const buffer = useAtomValue(schedulerRunEventsAtom);
  const onEventRef = useRef(onEvent);
  const cursorRef = useRef<number | null>(null);
  onEventRef.current = onEvent;

  // A newly mounted consumer hydrates canonical state through its own API
  // request. Start at the current tail so old events from another route or
  // project are not replayed into that snapshot.
  if (cursorRef.current === null) {
    cursorRef.current = buffer.sequence;
  }

  useEffect(() => {
    const firstSequence = buffer.events[0]?.sequence ?? buffer.sequence + 1;
    const startIndex = Math.max(0, (cursorRef.current ?? 0) - firstSequence + 1);
    for (let index = startIndex; index < buffer.events.length; index += 1) {
      const entry = buffer.events[index];
      if (!entry) continue;
      cursorRef.current = entry.sequence;
      onEventRef.current(entry.event);
    }
  }, [buffer]);
}
