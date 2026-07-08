import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/redesign/$")({
  beforeLoad: ({ location }) => {
    throw redirect({ href: location.href.replace(/^\/redesign(?=\/|$)/, "") || "/" });
  },
});
