"use client";

import { useEffect, useMemo, useState } from "react";

export function useWorkspaceTheme() {
  const [theme, setTheme] = useState<"light" | "dark">("light");

  useEffect(() => {
    const stored = window.localStorage.getItem("bruin-web-theme");
    const resolved =
      stored === "dark" || stored === "light"
        ? stored
        : window.matchMedia("(prefers-color-scheme: dark)").matches
          ? "dark"
          : "light";
    setTheme(resolved);
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("dark", theme === "dark");
    window.localStorage.setItem("bruin-web-theme", theme);
  }, [theme]);

  const monacoTheme = useMemo(
    () => (theme === "dark" ? "bruin-vs-dark" : "bruin-vs"),
    [theme]
  );

  return { theme, setTheme, monacoTheme };
}
