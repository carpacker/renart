"use client";

import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useSetAtom } from "jotai";
import { useCallback, useEffect, useMemo } from "react";

import {
  findExecutionTimeOption,
  getExecutionTimeOptions,
} from "@/lib/execution-time";
import { selectedExecutionTimeWindowAtom } from "@/lib/atoms/domains/workspace";
import { WebPipeline } from "@/lib/types";

export function useWorkspaceExecutionTime(pipeline: WebPipeline | null) {
  const setSelectedExecutionTimeWindow = useSetAtom(selectedExecutionTimeWindowAtom);
  const navigate = useNavigate();
  const routeState = useRouterState({
    select: (state) => ({
      pathname: state.location.pathname,
      search: state.location.search as { time?: string },
    }),
  });

  const options = useMemo(
    () => getExecutionTimeOptions(pipeline?.schedule),
    [pipeline?.schedule]
  );
  const selectedOption = useMemo(
    () => findExecutionTimeOption(options, routeState.search.time),
    [options, routeState.search.time]
  );

  useEffect(() => {
    setSelectedExecutionTimeWindow(
      selectedOption ? { start: selectedOption.start, end: selectedOption.end } : null
    );
  }, [selectedOption, setSelectedExecutionTimeWindow]);

  const handleExecutionTimeChange = useCallback(
    (value: string) => {
      const defaultValue = options[0]?.value;
      void navigate({
        to: routeState.pathname,
        search: {
          ...routeState.search,
          time: value === defaultValue ? undefined : value,
        },
        replace: true,
      });
    },
    [navigate, options, routeState.pathname, routeState.search]
  );

  return {
    options,
    selectedOption,
    selectedValue: selectedOption?.value ?? "",
    handleExecutionTimeChange,
  };
}
