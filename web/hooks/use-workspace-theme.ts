"use client";

import { type SetStateAction, useCallback, useEffect, useMemo, useState } from "react";

type WorkspaceTheme = "light" | "dark";

const themeChangeEvent = "renart-theme-change";

function resolveWorkspaceTheme(): WorkspaceTheme {
  if (typeof window === "undefined") {
    return "light";
  }

  const stored = window.localStorage.getItem("renart-theme");
  if (stored === "dark" || stored === "light") {
    return stored;
  }

  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function applyWorkspaceTheme(theme: WorkspaceTheme) {
  const root = document.documentElement;
  root.classList.toggle("dark", theme === "dark");
  window.localStorage.setItem("renart-theme", theme);
}

export function useWorkspaceTheme() {
  const [theme, setThemeState] = useState<WorkspaceTheme>(resolveWorkspaceTheme);

  useEffect(() => {
    const syncTheme = () => setThemeState(resolveWorkspaceTheme());
    const syncStorageTheme = (event: StorageEvent) => {
      if (event.key === "renart-theme") {
        syncTheme();
      }
    };
    const syncSystemTheme = () => {
      const stored = window.localStorage.getItem("renart-theme");
      if (stored !== "dark" && stored !== "light") {
        syncTheme();
      }
    };
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
    applyWorkspaceTheme(theme);
    window.dispatchEvent(new Event(themeChangeEvent));
  }, [theme]);

  const setTheme = useCallback((nextTheme: SetStateAction<WorkspaceTheme>) => {
    setThemeState((currentTheme) =>
      typeof nextTheme === "function" ? nextTheme(currentTheme) : nextTheme
    );
  }, []);

  const monacoTheme = useMemo(
    () => (theme === "dark" ? "bruin-vs-dark" : "bruin-vs"),
    [theme]
  );

  return { theme, setTheme, monacoTheme };
}
