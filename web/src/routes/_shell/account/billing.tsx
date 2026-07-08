import { createFileRoute } from "@tanstack/react-router";

import { AppAccountBillingPage } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/account/billing")({
  component: AppAccountBillingPage,
});
