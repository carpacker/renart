import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute(
  "/_workspace/settings/environments/$environmentId/connections/$connectionId/edit"
)({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/settings/environments/$environmentId/connections/$connectionId",
      params: {
        environmentId: params.environmentId,
        connectionId: params.connectionId,
      },
      replace: true,
    });
  },
});
