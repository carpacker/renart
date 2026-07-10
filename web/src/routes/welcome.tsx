import { createFileRoute } from "@tanstack/react-router";

import { WelcomePage } from "@/components/app/welcome-page";

type WelcomeSearch = {
  new?: boolean;
};

export const Route = createFileRoute("/welcome")({
  validateSearch: (search: Record<string, unknown>): WelcomeSearch => ({
    new: search.new === true || search.new === "1" || search.new === 1 ? true : undefined,
  }),
  component: WelcomeRoute,
});

function WelcomeRoute() {
  const search = Route.useSearch();
  return <WelcomePage forceNew={Boolean(search.new)} />;
}
