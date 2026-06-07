import { Outlet, createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/redesign/runs")({
  component: Outlet,
});
