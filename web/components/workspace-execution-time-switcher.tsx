"use client";

import { Clock3 } from "lucide-react";

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useWorkspaceExecutionTime } from "@/hooks/use-workspace-execution-time";
import { WebPipeline } from "@/lib/types";

export function WorkspaceExecutionTimeSwitcher({ pipeline }: { pipeline: WebPipeline | null }) {
  const { options, selectedOption, selectedValue, handleExecutionTimeChange } =
    useWorkspaceExecutionTime(pipeline);
  const groups = groupOptionsByDay(options);
  const hasMultiplePartitionsPerDay = groups.some((group) => group.options.length > 1);

  if (options.length === 0) {
    return null;
  }

  return (
    <Select value={selectedValue} onValueChange={handleExecutionTimeChange}>
      <SelectTrigger aria-label="Execution time" className="h-8 w-9 min-w-0 bg-muted/20 px-2 text-xs sm:w-auto sm:min-w-40 sm:px-3">
        <span className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden text-left">
          <Clock3 className="size-3 shrink-0 text-muted-foreground" />
          <SelectValue className="hidden sm:inline" placeholder="Time period">
            {selectedOption ? `Time: ${selectedOption.label}` : "Time period"}
          </SelectValue>
        </span>
      </SelectTrigger>
      <SelectContent align="end" position="popper">
        {hasMultiplePartitionsPerDay
          ? groups.map((group, index) => (
              <SelectGroup key={group.day}>
                <SelectLabel>{group.day}</SelectLabel>
                {group.options.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}{option.isDefault ? " (default)" : ""}
                  </SelectItem>
                ))}
                {index < groups.length - 1 ? <SelectSeparator /> : null}
              </SelectGroup>
            ))
          : options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}{option.isDefault ? " (default)" : ""}
              </SelectItem>
            ))}
      </SelectContent>
    </Select>
  );
}

function groupOptionsByDay<T extends { start: string }>(options: T[]) {
  const formatter = new Intl.DateTimeFormat(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
  });
  const groups: Array<{ day: string; options: T[] }> = [];
  for (const option of options) {
    const day = formatter.format(new Date(option.start));
    const current = groups[groups.length - 1];
    if (current?.day === day) {
      current.options.push(option);
    } else {
      groups.push({ day, options: [option] });
    }
  }
  return groups;
}
