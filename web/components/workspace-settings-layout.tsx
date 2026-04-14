"use client";

import { Outlet } from "@tanstack/react-router";
import { createContext, useContext } from "react";

import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";

type WorkspaceSettingsContextValue = ReturnType<typeof useWorkspaceSettingsData>;

const WorkspaceSettingsContext =
  createContext<WorkspaceSettingsContextValue | null>(null);

export function WorkspaceSettingsLayout() {
  const value = useWorkspaceSettingsData();

  return (
    <WorkspaceSettingsContext.Provider value={value}>
      <Outlet />
    </WorkspaceSettingsContext.Provider>
  );
}

export function useWorkspaceSettingsLayout() {
  const context = useContext(WorkspaceSettingsContext);

  if (!context) {
    throw new Error(
      "useWorkspaceSettingsLayout must be used within WorkspaceSettingsLayout"
    );
  }

  return context;
}
