import { createFileRoute } from "@tanstack/react-router";

import { RedesignAccountBillingPage } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/account/billing")({
  component: RedesignAccountBillingPage,
});
