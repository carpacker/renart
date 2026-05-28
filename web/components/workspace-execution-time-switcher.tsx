"use client";

import { Clock3 } from "lucide-react";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useWorkspaceExecutionTime } from "@/hooks/use-workspace-execution-time";
import { WebPipeline } from "@/lib/types";

export function WorkspaceExecutionTimeSwitcher({ pipeline }: { pipeline: WebPipeline | null }) {
  const { options, selectedOption, selectedValue, handleExecutionTimeChange } =
    useWorkspaceExecutionTime(pipeline);

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
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}{option.isDefault ? " (default)" : ""}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
