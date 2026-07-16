"use client";

import { type SetStateAction, useCallback, useEffect, useMemo, useState } from "react";

type WorkspaceTheme = "light" | "dark";
export type WorkspaceThemePreference = WorkspaceTheme | "system";

const themeChangeEvent = "renart-theme-change";

function resolveSystemTheme(): WorkspaceTheme {
  if (typeof window === "undefined") {
    return "light";
  }

  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function resolveThemePreference(): WorkspaceThemePreference {
  if (typeof window === "undefined") {
    return "system";
  }
  const stored = window.localStorage.getItem("renart-theme");
  if (stored === "dark" || stored === "light" || stored === "system") {
    return stored;
  }
  return "system";
}

function applyWorkspaceTheme(theme: WorkspaceTheme, preference: WorkspaceThemePreference) {
  const root = document.documentElement;
  root.classList.toggle("dark", theme === "dark");
  root.style.colorScheme = theme;
  window.localStorage.setItem("renart-theme", preference);
}

export function useWorkspaceTheme() {
  const [themePreference, setThemePreferenceState] =
    useState<WorkspaceThemePreference>(resolveThemePreference);
  const [systemTheme, setSystemTheme] = useState<WorkspaceTheme>(resolveSystemTheme);
  const theme = themePreference === "system" ? systemTheme : themePreference;

  useEffect(() => {
    const syncTheme = () => setThemePreferenceState(resolveThemePreference());
    const syncStorageTheme = (event: StorageEvent) => {
      if (event.key === "renart-theme") {
        syncTheme();
      }
    };
    const syncSystemTheme = () => setSystemTheme(resolveSystemTheme());
    const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");

    window.addEventListener(themeChangeEvent, syncTheme);
    window.addEventListener("storage", syncStorageTheme);
    systemTheme.addEventListener("change", syncSystemTheme);

    return () => {
      window.removeEventListener(themeChangeEvent, syncTheme);
      window.removeEventListener("storage", syncStorageTheme);
      systemTheme.removeEventListener("change", syncSystemTheme);
    };
  }, []);

  useEffect(() => {
    applyWorkspaceTheme(theme, themePreference);
    window.dispatchEvent(new Event(themeChangeEvent));
  }, [theme, themePreference]);

  const setTheme = useCallback((nextTheme: SetStateAction<WorkspaceThemePreference>) => {
    setThemePreferenceState((currentTheme) =>
      typeof nextTheme === "function" ? nextTheme(currentTheme) : nextTheme,
    );
  }, []);

  const monacoTheme = useMemo(() => (theme === "dark" ? "bruin-vs-dark" : "bruin-vs"), [theme]);

  return { theme, themePreference, setTheme, monacoTheme };
}
