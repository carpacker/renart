"use client";

import { Database } from "lucide-react";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useWorkspaceEnvironment } from "@/hooks/use-workspace-environment";

export function WorkspaceEnvironmentSwitcher() {
  const {
    availableEnvironmentNames,
    handleEnvironmentChange,
    selectedEnvironment,
    selectedEnvironmentLabel,
  } = useWorkspaceEnvironment();

  return (
    <div className="flex min-w-0 shrink items-center gap-2">
      <Select
        value={selectedEnvironment || "__default__"}
        onValueChange={handleEnvironmentChange}
      >
        <SelectTrigger
          aria-label="Environment"
          className="h-8 w-9 min-w-0 bg-muted/20 px-2 text-xs sm:w-auto sm:min-w-36 sm:px-3"
        >
          <span className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden text-left">
            <Database className="size-3 shrink-0 text-muted-foreground" />
            <SelectValue className="hidden sm:inline" placeholder="Environment">
              {`Environment: ${selectedEnvironmentLabel}`}
            </SelectValue>
          </span>
        </SelectTrigger>
        <SelectContent align="end" position="popper">
          <SelectItem value="__default__">default</SelectItem>
          {availableEnvironmentNames.map((environmentName) => (
            <SelectItem key={environmentName} value={environmentName}>
              {environmentName}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
