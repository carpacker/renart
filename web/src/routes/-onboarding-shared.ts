import { redirect } from "@tanstack/react-router";

import { getOnboardingState } from "@/lib/api";

export async function loadOnboardingRouteContext() {
  const state = await getOnboardingState();
  if (!state.active) {
    throw redirect({
      to: "/",
      search: {
        pipeline: undefined,
        asset: undefined,
        environment: undefined,
      },
    });
  }

  return { onboardingState: state };
}
