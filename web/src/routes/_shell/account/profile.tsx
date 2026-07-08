import { createFileRoute } from "@tanstack/react-router";

import { AppAccountProfilePage } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/account/profile")({
  component: AppAccountProfilePage,
});
