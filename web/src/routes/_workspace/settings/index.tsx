import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_workspace/settings/")({
  validateSearch: (search: Record<string, unknown>) => ({
    environment:
      typeof search.environment === "string" ? search.environment : undefined,
  }),
  beforeLoad: ({ search }) => {
    if (search.environment) {
      throw redirect({
        to: "/settings/environments/$environmentId",
        params: { environmentId: search.environment },
        search: { environment: search.environment },
      });
    }

    throw redirect({ to: "/settings/environments" });
  },
});
