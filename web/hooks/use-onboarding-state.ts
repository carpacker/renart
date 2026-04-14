"use client";

import { useMemo } from "react";

import { getAssetViewMode } from "@/lib/asset-visualization";
import { WebAsset, WebPipeline } from "@/lib/types";

export type OnboardingHelpTarget =
  | "sidebar"
  | "canvas"
  | "editor"
  | "actions"
  | "visualization";

export type OnboardingStep = {
  id:
    | "pipeline"
    | "python"
    | "sql"
    | "sql-query"
    | "materialize"
    | "visualization";
  label: string;
  description: string;
  done: boolean;
};

export type OnboardingState = {
  pythonAssets: WebAsset[];
  sqlAssets: WebAsset[];
  primaryPythonAsset: WebAsset | null;
  primarySQLAsset: WebAsset | null;
  sqlAssetReferencingPython: WebAsset | null;
  sqlDraftTarget: WebAsset | null;
  tutorialAssets: WebAsset[];
  hasVisualization: boolean;
  steps: OnboardingStep[];
  completedStepCount: number;
  progress: number;
};

export type OnboardingHelp = {
  target: OnboardingHelpTarget;
  message: string;
};

export function useOnboardingState(
  pipeline: WebPipeline | null
): {
  onboarding: OnboardingState;
  activeOnboardingStep: OnboardingStep | null;
  onboardingHelp: OnboardingHelp;
} {
  const onboarding = useMemo<OnboardingState>(() => {
    const assets = pipeline?.assets ?? [];
    const pythonAssets = assets.filter(isPythonAsset);
    const sqlAssets = assets.filter(isSQLAsset);
    const primaryPythonAsset = pythonAssets[0] ?? null;
    const primarySQLAsset = sqlAssets[0] ?? null;
    const sqlAssetReferencingPython = findSQLAssetReferencingPython(
      sqlAssets,
      primaryPythonAsset
    );
    const sqlDraftTarget = sqlAssetReferencingPython ?? primarySQLAsset;
    const tutorialAssets = compactAssetList([primaryPythonAsset, primarySQLAsset]);
    const hasVisualization = primarySQLAsset
      ? getAssetViewMode(primarySQLAsset.meta) !== null
      : false;
    const allTutorialMaterialized =
      tutorialAssets.length === 2 &&
      tutorialAssets.every((current) => current.is_materialized);

    const steps: OnboardingStep[] = [
      {
        id: "pipeline",
        label: "Create A Pipeline",
        description:
          "Use the left sidebar to create a new pipeline (or select an existing one).",
        done: Boolean(pipeline),
      },
      {
        id: "python",
        label: "Create A Python Asset",
        description:
          "Click the canvas, choose Python, enter a name, and create the asset.",
        done: pythonAssets.length >= 1,
      },
      {
        id: "sql",
        label: "Create A SQL Asset",
        description:
          "Click the canvas again, choose SQL, and create one SQL asset.",
        done: sqlAssets.length >= 1,
      },
      {
        id: "sql-query",
        label: "Write The SQL Query",
        description:
          "Open the SQL asset and query from your Python asset (starter SQL can fill this in).",
        done: Boolean(sqlAssetReferencingPython && primaryPythonAsset),
      },
      {
        id: "materialize",
        label: "Materialize Both Assets",
        description:
          "Run materialization for both tutorial assets to create their outputs.",
        done: allTutorialMaterialized,
      },
      {
        id: "visualization",
        label: "Add A Visualization",
        description:
          "Open the Visualization tab on the SQL asset and save a table or chart view.",
        done: hasVisualization,
      },
    ];
    const completedStepCount = steps.filter((step) => step.done).length;

    return {
      pythonAssets,
      sqlAssets,
      primaryPythonAsset,
      primarySQLAsset,
      sqlAssetReferencingPython,
      sqlDraftTarget,
      tutorialAssets,
      hasVisualization,
      steps,
      completedStepCount,
      progress:
        steps.length === 0 ? 0 : (completedStepCount / steps.length) * 100,
    };
  }, [pipeline]);

  const activeOnboardingStep = useMemo(
    () => onboarding.steps.find((step) => !step.done) ?? null,
    [onboarding.steps]
  );

  const onboardingHelp = useMemo<OnboardingHelp>(() => {
    if (!activeOnboardingStep) {
      return {
        target: "actions",
        message:
          "Onboarding complete. You can continue exploring: inspect data, materialize again, or tweak visualizations.",
      };
    }

    switch (activeOnboardingStep.id) {
      case "pipeline":
        return {
          target: "sidebar",
          message:
            "Use the left sidebar and click New to create a pipeline, then select it.",
        };
      case "python":
        return {
          target: "canvas",
          message:
            "Click the canvas to open the new-asset card, switch to Python, name it, and click Create.",
        };
      case "sql":
        return {
          target: "canvas",
          message:
            "Click the canvas again, switch the card to SQL, enter a name, and click Create.",
        };
      case "sql-query":
        return {
          target: "editor",
          message:
            "Select the SQL asset and write a query that reads from your Python asset table. The SQL starter button can generate this.",
        };
      case "materialize":
        return {
          target: "actions",
          message:
            "Use Materialize Tutorial Assets in onboarding, or materialize each asset with the top action button.",
        };
      case "visualization":
        return {
          target: "visualization",
          message:
            "Open the Visualization tab and save a table/chart view (or use Add Starter Visualization).",
        };
      default:
        return {
          target: "actions",
          message: "Continue with the next onboarding action.",
        };
    }
  }, [activeOnboardingStep]);

  return {
    onboarding,
    activeOnboardingStep,
    onboardingHelp,
  };
}

function isPythonAsset(asset: WebAsset): boolean {
  const type = (asset.type ?? "").toLowerCase();
  const path = (asset.path ?? "").toLowerCase();
  return type.includes("python") || path.endsWith(".py");
}

function isSQLAsset(asset: WebAsset): boolean {
  const type = (asset.type ?? "").toLowerCase();
  const path = (asset.path ?? "").toLowerCase();
  return type.includes("sql") || path.endsWith(".sql");
}

function compactAssetList(assets: Array<WebAsset | null>): WebAsset[] {
  const dedup = new Map<string, WebAsset>();
  for (const asset of assets) {
    if (!asset) {
      continue;
    }
    dedup.set(asset.id, asset);
  }
  return Array.from(dedup.values());
}

function normalizedAssetNameTokens(assetName: string): string[] {
  const normalized = assetName.trim().toLowerCase();
  if (!normalized) {
    return [];
  }

  const tokens = new Set<string>([normalized]);
  if (normalized.includes(".")) {
    const parts = normalized.split(".").filter(Boolean);
    tokens.add(parts[parts.length - 1] ?? "");
  }

  return Array.from(tokens).filter(Boolean);
}

function findSQLAssetReferencingPython(
  sqlAssets: WebAsset[],
  pythonAsset: WebAsset | null
): WebAsset | null {
  if (!pythonAsset) {
    return null;
  }

  for (const sqlCandidate of sqlAssets) {
    const contentLower = (sqlCandidate.content ?? "").toLowerCase();
    if (!contentLower) {
      continue;
    }

    const pythonReferenced = normalizedAssetNameTokens(pythonAsset.name).some(
      (token) => contentLower.includes(token)
    );
    if (pythonReferenced) {
      return sqlCandidate;
    }
  }

  return null;
}
