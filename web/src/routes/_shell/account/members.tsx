import { createFileRoute } from "@tanstack/react-router";

import { AppAccountMembersPage } from "@/components/app/settings-pages";

export const Route = createFileRoute("/_shell/account/members")({
  component: AppAccountMembersPage,
});
