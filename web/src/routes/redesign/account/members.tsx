import { createFileRoute } from "@tanstack/react-router";

import { RedesignAccountMembersPage } from "@/components/redesign/settings-pages";

export const Route = createFileRoute("/redesign/account/members")({
  component: RedesignAccountMembersPage,
});
