"use client";

import { CheckCircle2, Circle, CircleHelp, Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { OnboardingHelp, OnboardingState } from "@/hooks/use-onboarding-state";

type Props = {
  showOnboarding: boolean;
  helpMode: boolean;
  onboarding: OnboardingState;
  onboardingHelp: OnboardingHelp;
  pipelineExists: boolean;
  onboardingMaterializeLoading: boolean;
  onToggleHelp: () => void;
  onHide: () => void;
  onShow: () => void;
  onCreatePipeline: () => void;
  onCreateOnboardingAsset: (kind: "python" | "sql") => void;
  onApplyPythonStarter: () => void;
  onApplySQLStarter: () => void;
  onMaterializeOnboardingAssets: () => void;
  onApplyVisualizationStarter: () => void;
};

export function WorkspaceOnboardingPanel({
  showOnboarding,
  helpMode,
  onboarding,
  onboardingHelp,
  pipelineExists,
  onboardingMaterializeLoading,
  onToggleHelp,
  onHide,
  onShow,
  onCreatePipeline,
  onCreateOnboardingAsset,
  onApplyPythonStarter,
  onApplySQLStarter,
  onMaterializeOnboardingAssets,
  onApplyVisualizationStarter,
}: Props) {
  if (!showOnboarding) {
    return (
      <div className="flex items-center justify-between rounded-md border bg-muted/20 p-2">
        <span className="text-xs opacity-80">Onboarding hidden</span>
        <Button onClick={onShow} size="sm" type="button" variant="outline">
          Show
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-3 rounded-md border bg-muted/20 p-3">
      <div className="flex items-center justify-between">
        <div className="text-xs font-semibold uppercase tracking-wide opacity-80">
          Interactive Onboarding
        </div>
        <div className="flex items-center gap-2">
          <Button
            onClick={onToggleHelp}
            size="sm"
            type="button"
            variant={helpMode ? "default" : "outline"}
          >
            Help
          </Button>
          <Button onClick={onHide} size="sm" type="button" variant="ghost">
            Hide
          </Button>
        </div>
      </div>

      <p className="text-xs opacity-80">
        Build one Python asset and one SQL asset, materialize them, then add a
        visualization to the SQL output.
      </p>
      <div className="text-xs opacity-70">
        {onboarding.completedStepCount}/{onboarding.steps.length} complete
      </div>
      <Progress className="h-1.5" value={onboarding.progress} />

      {helpMode && (
        <div className="rounded-md border border-primary/50 bg-primary/10 px-2 py-1.5 text-xs">
          <span className="font-semibold">Next:</span> {onboardingHelp.message}
        </div>
      )}

      <div className="space-y-1.5">
        {onboarding.steps.map((step) => (
          <div className="flex items-start gap-2 text-xs" key={step.id}>
            {step.done ? (
              <CheckCircle2 className="mt-0.5 size-3.5 text-emerald-600" />
            ) : (
              <Circle className="mt-0.5 size-3.5 text-muted-foreground" />
            )}
            <div className="flex items-center gap-1">
              <div className="font-medium">{step.label}</div>
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      aria-label={`How to complete: ${step.label}`}
                      className="inline-flex text-muted-foreground hover:text-foreground"
                      type="button"
                    >
                      <CircleHelp className="size-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="right" sideOffset={6}>
                    {step.description}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
          </div>
        ))}
      </div>

      <div className="flex flex-wrap gap-2">
        {!pipelineExists && (
          <Button onClick={onCreatePipeline} size="sm" type="button" variant="outline">
            Create Pipeline
          </Button>
        )}

        {pipelineExists && onboarding.pythonAssets.length === 0 && (
          <Button
            onClick={() => onCreateOnboardingAsset("python")}
            size="sm"
            type="button"
            variant="outline"
          >
            Add Python Asset
          </Button>
        )}

        {pipelineExists && onboarding.sqlAssets.length < 1 && (
          <Button
            onClick={() => onCreateOnboardingAsset("sql")}
            size="sm"
            type="button"
            variant="outline"
          >
            Add SQL Asset
          </Button>
        )}

        {pipelineExists && onboarding.primaryPythonAsset && (
          <Button onClick={onApplyPythonStarter} size="sm" type="button" variant="outline">
            <Sparkles className="mr-1 size-3.5" />
            Python Starter Query
          </Button>
        )}

        {pipelineExists && onboarding.sqlDraftTarget && onboarding.primaryPythonAsset && (
          <Button onClick={onApplySQLStarter} size="sm" type="button" variant="outline">
            <Sparkles className="mr-1 size-3.5" />
            SQL Starter Query
          </Button>
        )}

        {pipelineExists && onboarding.tutorialAssets.length === 2 && (
          <Button
            disabled={onboardingMaterializeLoading}
            onClick={onMaterializeOnboardingAssets}
            size="sm"
            type="button"
            variant="outline"
          >
            {onboardingMaterializeLoading
              ? "Materializing..."
              : "Materialize Both Assets"}
          </Button>
        )}

        {pipelineExists && onboarding.primarySQLAsset && (
          <Button
            onClick={onApplyVisualizationStarter}
            size="sm"
            type="button"
            variant="outline"
          >
            Add Starter Visualization
          </Button>
        )}
      </div>
    </div>
  );
}
