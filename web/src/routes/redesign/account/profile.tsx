import { createFileRoute } from "@tanstack/react-router";

import { RedesignAccountProfilePage } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/account/profile")({
  component: RedesignAccountProfilePage,
});
