"use client";

import { createContext, useContext } from "react";

export const portalContainerContext = createContext<HTMLElement | null>(null);

export function usePortalContainer() {
  return useContext(portalContainerContext);
}
