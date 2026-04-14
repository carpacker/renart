import { Outlet, createRootRoute, useLocation } from "@tanstack/react-router";
import { useEffect } from "react";

import { AppProviders } from "@/src/providers";

export const Route = createRootRoute({
  component: RootComponent,
});

function RootComponent() {
  const location = useLocation();

  useEffect(() => {
    document.title = getDocumentTitle(location.pathname);
  }, [location.pathname]);

  return (
    <AppProviders>
      <Outlet />
    </AppProviders>
  );
}

function getDocumentTitle(pathname: string) {
  if (pathname.startsWith("/settings/connections")) {
    return "Connections · Settings · Bruin Web";
  }

  if (pathname.startsWith("/settings/environments") || pathname === "/settings") {
    return "Environments · Settings · Bruin Web";
  }

  return "Workspace · Bruin Web";
}
