import { createFileRoute } from "@tanstack/react-router";

import { OnboardingRoutePage } from "@/components/onboarding-route-page";
import { loadOnboardingRouteContext } from "@/src/routes/-onboarding-shared";

export const Route = createFileRoute("/onboarding")({
  beforeLoad: loadOnboardingRouteContext,
  component: OnboardingRouteComponent,
});

function OnboardingRouteComponent() {
  const { onboardingState } = Route.useRouteContext();
  return <OnboardingRoutePage onboardingState={onboardingState} />;
}
