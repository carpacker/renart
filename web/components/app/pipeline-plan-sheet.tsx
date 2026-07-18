"use client";

import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  CheckCircle2,
  CircleAlert,
  FileCode2,
  Loader2,
  Play,
  ShieldAlert,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  ReadOnlyRenderedOperation,
  assetRenderStageLabel,
} from "@/components/app/asset-render-view";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { APIError } from "@/lib/api-core";
import {
  canonicalPipelinePlanRequest,
  canonicalPipelinePlanReviewedIdentity,
  confirmPipelinePlan,
  pipelinePlanFromConflict,
  planPipeline,
  type PipelinePlan,
  type PipelinePlanRequest,
} from "@/lib/api-pipeline-plan";
import { activePipelineRunConflict, type PipelineRunSource } from "@/lib/api-scheduler";
import type { PipelineRun } from "@/lib/types";
import { awaitWorkspaceSaves } from "@/lib/workspace-save-barrier";
import { cn } from "@/lib/utils";

type PlanSelectionMode = "all" | "needed";
type SensorMode = "once" | "wait" | "skip";

export function PipelinePlanSheet({
  open,
  onOpenChange,
  pipelineId,
  pipelineName,
  environment,
  timeWindow,
  source,
  confirmDestructive = false,
  onAccepted,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pipelineId: string;
  pipelineName: string;
  environment: string;
  timeWindow?: { start: string; end: string } | null;
  source?: PipelineRunSource | null;
  confirmDestructive?: boolean;
  onAccepted: (run: PipelineRun, plan: PipelinePlan) => void;
}) {
  const [request, setRequest] = useState<PipelinePlanRequest | null>(null);
  const [plan, setPlan] = useState<PipelinePlan | null>(null);
  const [loading, setLoading] = useState(false);
  const [contentLoading, setContentLoading] = useState(false);
  const [stageContentLoaded, setStageContentLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState("summary");
  const requestSerial = useRef(0);
  const initialPlanContext = useRef<string | null>(null);
  const requestedSourceKind = source?.source;
  const requestedSourceVersion =
    source?.source === "snapshot" ? source.snapshot_version_id : undefined;

  const fetchPlan = useCallback(
    async (input: PipelinePlanRequest, includeStageContent = false) => {
      const serial = ++requestSerial.current;
      if (includeStageContent) setContentLoading(true);
      else setLoading(true);
      setError(null);
      setActiveRunId(null);
      try {
        const next = await planPipeline(pipelineId, {
          ...input,
          include_stage_content: includeStageContent,
        });
        if (serial !== requestSerial.current) return;
        setPlan(next);
        setRequest(canonicalPipelinePlanRequest(next, false));
        setStageContentLoaded(includeStageContent);
      } catch (cause) {
        if (serial !== requestSerial.current) return;
        setError(cause instanceof Error ? cause.message : "Pipeline planning failed.");
      } finally {
        if (serial === requestSerial.current) {
          setLoading(false);
          setContentLoading(false);
        }
      }
    },
    [pipelineId],
  );

  useEffect(() => {
    if (!open) {
      initialPlanContext.current = null;
      requestSerial.current += 1;
      return;
    }
    if (initialPlanContext.current !== null) return;
    initialPlanContext.current = "open";
    setPlan(null);
    setRequest(null);
    setError(null);
    setActiveRunId(null);
    setConfirmation("");
    setActiveTab("summary");
    setStageContentLoaded(false);
    setLoading(true);
    const serial = ++requestSerial.current;
    void (async () => {
      try {
        await awaitWorkspaceSaves();
        if (serial !== requestSerial.current) return;
        const input: PipelinePlanRequest = {
          environment: environment || undefined,
          start_date: timeWindow?.start,
          end_date: timeWindow?.end,
          execution_time: new Date().toISOString(),
          sensor_mode: "once",
          source: requestedSourceKind
            ? {
                kind: requestedSourceKind,
                version_id: requestedSourceVersion,
              }
            : undefined,
          selection: { mode: "all" },
        };
        setRequest(input);
        await fetchPlan(input);
      } catch (cause) {
        if (serial !== requestSerial.current) return;
        setError(cause instanceof Error ? cause.message : "Saving the workspace failed.");
        setLoading(false);
      }
    })();
  }, [
    environment,
    fetchPlan,
    open,
    pipelineId,
    requestedSourceKind,
    requestedSourceVersion,
    timeWindow?.end,
    timeWindow?.start,
  ]);

  const updateRequest = (update: (current: PipelinePlanRequest) => PipelinePlanRequest) => {
    if (!request) return;
    const next = update(request);
    setRequest(next);
    setStageContentLoaded(false);
    void fetchPlan(next);
  };

  const selectionMode = (request?.selection?.mode ??
    plan?.selection.mode ??
    "all") as PlanSelectionMode;
  const sensorMode = (request?.sensor_mode ?? plan?.context.sensor_mode ?? "once") as SensorMode;
  const fullRefresh = Boolean(request?.full_refresh ?? plan?.context.requested_full_refresh);
  const destructiveConfirmationRequired = Boolean(confirmDestructive && plan?.context.destructive);
  const confirmationMatches =
    !destructiveConfirmationRequired || confirmation.trim() === plan?.context.environment;
  const hasBlockers = Boolean(
    plan && (plan.status === "blocked" || plan.readiness.blockers.length),
  );
  const canConfirm = Boolean(
    plan && !hasBlockers && confirmationMatches && !loading && plan.summary.execution_units > 0,
  );

  const loadStageContent = () => {
    if (!plan || stageContentLoaded || contentLoading) return;
    void fetchPlan(canonicalPipelinePlanRequest(plan, true), true);
  };

  const confirm = async () => {
    if (!plan || !canConfirm) return;
    setConfirming(true);
    setError(null);
    setActiveRunId(null);
    try {
      const response = await confirmPipelinePlan(pipelineId, {
        plan_id: plan.id,
        plan: canonicalPipelinePlanRequest(plan, false),
        reviewed: canonicalPipelinePlanReviewedIdentity(plan),
        confirmed_environment: destructiveConfirmationRequired ? confirmation.trim() : undefined,
      });
      onAccepted(response.run, plan);
      onOpenChange(false);
    } catch (cause) {
      const refreshed = pipelinePlanFromConflict(cause);
      if (refreshed) {
        setPlan(refreshed);
        setRequest(canonicalPipelinePlanRequest(refreshed, false));
        setStageContentLoaded(false);
        setError(
          cause instanceof APIError && cause.code === "plan_data_changed"
            ? "The data state now requires additional or changed work. Review the refreshed plan before running."
            : cause instanceof APIError && cause.code === "plan_stale"
              ? "The source or configuration changed. Review the refreshed plan before running."
              : cause instanceof Error
                ? cause.message
                : "The refreshed plan is blocked.",
        );
        return;
      }
      const active = activePipelineRunConflict(cause);
      if (active) {
        setActiveRunId(active.activeRunId);
        setError("Another run was admitted first. Open it to follow its progress.");
        return;
      }
      setError(cause instanceof Error ? cause.message : "Pipeline run could not be started.");
    } finally {
      setConfirming(false);
    }
  };

  const sourceLabel = plan ? planSourceLabel(plan) : sourceInputLabel(source);
  const finalActionLabel = plan
    ? `Run ${plan.summary.assets} ${plan.summary.assets === 1 ? "asset" : "assets"} from ${runSourceLabel(plan)}`
    : "Run pipeline";

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="w-full gap-0 p-0 sm:max-w-2xl lg:max-w-3xl"
        data-testid="pipeline-plan-sheet"
      >
        <SheetHeader className="shrink-0 border-b px-5 py-4 pr-12">
          <div className="flex min-w-0 items-center gap-2">
            <SheetTitle className="truncate">Review pipeline run</SheetTitle>
            {plan ? <PlanStatusBadge status={plan.status} /> : null}
            {(loading || contentLoading) && plan ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : null}
          </div>
          <SheetDescription>
            {pipelineName} · saved source preview · nothing executes until you confirm
          </SheetDescription>
        </SheetHeader>

        <div className="shrink-0 border-b px-5 py-3">
          <dl className="grid min-w-0 grid-cols-2 gap-x-6 gap-y-2 text-xs sm:grid-cols-4">
            <PlanContextItem label="Source" value={sourceLabel} />
            <PlanContextItem
              label="Environment"
              value={plan?.context.environment || environment || "default"}
            />
            <PlanContextItem
              label="Window"
              value={
                plan
                  ? formatPlanWindow(plan.context.start_date, plan.context.end_date)
                  : timeWindow
                    ? formatPlanWindow(timeWindow.start, timeWindow.end)
                    : "Resolving…"
              }
            />
            <PlanContextItem
              label="Mode"
              value={`${fullRefresh ? "full refresh" : "incremental"} · sensor ${sensorMode}`}
            />
          </dl>
          <div className="mt-3 flex flex-wrap items-end gap-3 border-t pt-3">
            <div className="min-w-40 flex-1 sm:max-w-52">
              <Label
                htmlFor="pipeline-plan-scope"
                className="mb-1 block text-[11px] text-muted-foreground"
              >
                Scope
              </Label>
              <Select
                value={selectionMode}
                onValueChange={(value) =>
                  updateRequest((current) => ({
                    ...current,
                    selection: { mode: value as PlanSelectionMode },
                  }))
                }
              >
                <SelectTrigger id="pipeline-plan-scope" size="sm" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Entire pipeline</SelectItem>
                  <SelectItem value="needed">Needed assets</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="min-w-36 flex-1 sm:max-w-44">
              <Label
                htmlFor="pipeline-plan-sensor"
                className="mb-1 block text-[11px] text-muted-foreground"
              >
                Sensors
              </Label>
              <Select
                value={sensorMode}
                onValueChange={(value) =>
                  updateRequest((current) => ({ ...current, sensor_mode: value as SensorMode }))
                }
              >
                <SelectTrigger id="pipeline-plan-sensor" size="sm" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="once">Check once</SelectItem>
                  <SelectItem value="wait">Wait</SelectItem>
                  <SelectItem value="skip">Skip</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Label className="flex h-8 items-center gap-2 rounded-md border px-3 text-xs font-normal">
              <Switch
                size="sm"
                checked={fullRefresh}
                onCheckedChange={(checked) =>
                  updateRequest((current) => ({ ...current, full_refresh: checked }))
                }
              />
              Full refresh
            </Label>
          </div>
        </div>

        {plan ? (
          <Tabs
            value={activeTab}
            className="min-h-0 flex-1 gap-0"
            onValueChange={(value) => {
              setActiveTab(value);
              if (value === "execution") loadStageContent();
            }}
          >
            <div className="shrink-0 border-b px-5 py-2">
              <TabsList variant="line" className="w-full justify-start">
                <TabsTrigger value="summary">Summary</TabsTrigger>
                <TabsTrigger value="assets">Assets</TabsTrigger>
                <TabsTrigger value="checks">Checks</TabsTrigger>
                <TabsTrigger value="execution">Execution</TabsTrigger>
              </TabsList>
            </div>
            <ScrollArea className="min-h-0 flex-1">
              <TabsContent value="summary" className="m-0 space-y-4 p-5">
                <PlanSummary plan={plan} />
              </TabsContent>
              <TabsContent value="assets" className="m-0 p-5">
                <PlanAssets plan={plan} />
              </TabsContent>
              <TabsContent value="checks" className="m-0 p-5">
                <PlanChecks plan={plan} />
              </TabsContent>
              <TabsContent value="execution" className="m-0 p-5">
                <PlanExecution
                  plan={plan}
                  contentLoading={contentLoading}
                  contentLoaded={stageContentLoaded}
                />
              </TabsContent>
            </ScrollArea>
          </Tabs>
        ) : (
          <div className="min-h-0 flex-1 p-5">
            {loading ? <PlanLoading /> : null}
            {!loading && error ? (
              <Alert variant="destructive">
                <AlertTriangle />
                <AlertTitle>Plan failed</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}
          </div>
        )}

        <SheetFooter className="shrink-0 gap-3 border-t bg-muted/10 px-5 py-4">
          {error && plan ? (
            <Alert variant="destructive">
              <AlertTriangle />
              <AlertTitle>Plan needs attention</AlertTitle>
              <AlertDescription>
                {error}{" "}
                {activeRunId ? (
                  <Link to="/runs/$runId" params={{ runId: activeRunId }}>
                    Open active run
                  </Link>
                ) : null}
              </AlertDescription>
            </Alert>
          ) : null}
          {destructiveConfirmationRequired ? (
            <div className="space-y-1.5 text-left">
              <Label htmlFor="pipeline-plan-confirm-environment">
                Type <span className="font-mono">{plan?.context.environment}</span> to confirm
                destructive operations
              </Label>
              <Input
                id="pipeline-plan-confirm-environment"
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
                autoComplete="off"
              />
            </div>
          ) : null}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="min-w-0 flex-1 text-left text-[11px] text-muted-foreground">
              {selectionMode === "needed"
                ? "Confirmation omits work that became fresh, but never adds new work without another review."
                : plan?.readiness.active_run_id
                  ? "This pipeline already has an active run."
                  : "Confirmation rechecks the complete plan before the run is admitted."}
            </div>
            <Button onClick={() => void confirm()} disabled={!canConfirm || confirming}>
              {confirming ? (
                <Loader2 data-icon="inline-start" className="animate-spin" />
              ) : (
                <Play data-icon="inline-start" />
              )}
              {confirming ? "Starting…" : finalActionLabel}
            </Button>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function PlanContextItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-[10px] font-medium tracking-wide text-muted-foreground uppercase">
        {label}
      </dt>
      <dd className="truncate font-medium" title={value}>
        {value}
      </dd>
    </div>
  );
}

function PlanStatusBadge({ status }: { status: string }) {
  return (
    <Badge
      variant={
        status === "blocked" ? "destructive" : status === "warning" ? "secondary" : "outline"
      }
      size="xs"
    >
      {status === "ready" ? (
        <CheckCircle2 data-icon="inline-start" />
      ) : (
        <CircleAlert data-icon="inline-start" />
      )}
      {status}
    </Badge>
  );
}

function PlanSummary({ plan }: { plan: PipelinePlan }) {
  return (
    <>
      <div className="grid grid-cols-2 divide-x divide-y rounded-lg border sm:grid-cols-4 sm:divide-y-0">
        <SummaryMetric label="Assets" value={plan.summary.assets} />
        <SummaryMetric label="Execution units" value={plan.summary.execution_units} />
        <SummaryMetric label="Operations" value={plan.summary.stages} />
        <SummaryMetric label="Destructive" value={plan.summary.destructive_operations} />
      </div>
      <PlanIssues title="Blockers" issues={plan.readiness.blockers} destructive />
      <PlanIssues title="Warnings" issues={plan.readiness.warnings} />
      {plan.readiness.blockers.length === 0 && plan.readiness.warnings.length === 0 ? (
        <Alert>
          <CheckCircle2 />
          <AlertTitle>Ready to run</AlertTitle>
          <AlertDescription>
            The saved source and complete operation graph passed planning.
          </AlertDescription>
        </Alert>
      ) : null}
      {plan.readiness.active_run_id ? (
        <Alert variant="destructive">
          <ShieldAlert />
          <AlertTitle>Pipeline already running</AlertTitle>
          <AlertDescription>
            <Link to="/runs/$runId" params={{ runId: plan.readiness.active_run_id }}>
              Open active run
            </Link>
          </AlertDescription>
        </Alert>
      ) : null}
      <div className="space-y-2 border-t pt-3 text-[11px] text-muted-foreground">
        <div className="flex flex-wrap gap-2">
          <Badge variant="muted" size="xs" title={plan.source.merkle_root}>
            Source {plan.source.merkle_root.slice(0, 8)}
          </Badge>
          {plan.context.configuration_digest ? (
            <Badge variant="muted" size="xs" title={plan.context.configuration_digest}>
              Config {plan.context.configuration_digest.slice(0, 8)}
            </Badge>
          ) : null}
          <Badge variant="muted" size="xs" title={plan.context.variables_digest}>
            Variables {plan.context.variables_digest.slice(0, 8)}
          </Badge>
        </div>
        <p>These identities are rechecked when you confirm and again before the first task.</p>
      </div>
    </>
  );
}

function SummaryMetric({ label, value }: { label: string; value: number }) {
  return (
    <div className="px-3 py-2.5">
      <div className="text-lg font-semibold tabular-nums">{value}</div>
      <div className="text-[10px] tracking-wide text-muted-foreground uppercase">{label}</div>
    </div>
  );
}

function PlanIssues({
  title,
  issues,
  destructive = false,
}: {
  title: string;
  issues: PipelinePlan["readiness"]["blockers"];
  destructive?: boolean;
}) {
  if (issues.length === 0) return null;
  return (
    <Alert
      variant={destructive ? "destructive" : "default"}
      className={cn(!destructive && "border-amber-500/40")}
    >
      <AlertTriangle />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        <ul className="space-y-1">
          {issues.map((issue, index) => (
            <li key={`${issue.code}:${issue.asset_id ?? index}`}>
              {issue.asset_name ? <span className="font-medium">{issue.asset_name}: </span> : null}
              {issue.message}
            </li>
          ))}
        </ul>
      </AlertDescription>
    </Alert>
  );
}

function PlanAssets({ plan }: { plan: PipelinePlan }) {
  if (plan.assets.length === 0) {
    return <p className="text-muted-foreground">No assets are selected for this plan.</p>;
  }
  return (
    <div className="divide-y border-y">
      {plan.assets.map((asset, index) => (
        <div key={asset.id} className="flex min-w-0 gap-3 py-3">
          <span className="w-5 shrink-0 text-right text-[10px] tabular-nums text-muted-foreground">
            {index + 1}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 flex-wrap items-center gap-1.5">
              <span className="truncate font-mono font-medium" title={asset.name}>
                {asset.name}
              </span>
              <Badge variant="outline" size="xs">
                {asset.type}
              </Badge>
              {asset.staleness ? (
                <Badge variant="muted" size="xs">
                  {asset.staleness.replaceAll("_", " ")}
                </Badge>
              ) : null}
            </div>
            <div className="mt-1 flex flex-wrap gap-1 text-[11px] text-muted-foreground">
              {asset.inclusion_reasons.map((reason) => (
                <span key={reason}>{reason.replaceAll("_", " ")}</span>
              ))}
              <span>
                · {asset.renders.length} render{asset.renders.length === 1 ? "" : "s"}
              </span>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

function PlanChecks({ plan }: { plan: PipelinePlan }) {
  const report = plan.readiness.code_checks;
  const runtimeChecks = plan.assets.flatMap((asset) =>
    asset.renders.flatMap((render) =>
      render.stages
        .filter((stage) => stage.kind === "check")
        .map((stage) => ({ asset: asset.name, stage })),
    ),
  );
  return (
    <div className="space-y-5">
      <section>
        <div className="mb-2 flex items-center justify-between">
          <h3 className="font-medium">Code checks</h3>
          <span className="text-[11px] text-muted-foreground">
            {report.summary.errors} errors · {report.summary.warnings} warnings
          </span>
        </div>
        <div className="divide-y border-y">
          {report.assets.map((asset) => (
            <div key={asset.name} className="py-2.5">
              <div className="font-mono font-medium">{asset.name}</div>
              {asset.findings.length === 0 ? (
                <div className="mt-0.5 text-[11px] text-muted-foreground">No findings</div>
              ) : (
                <ul className="mt-1 space-y-1">
                  {asset.findings.map((finding, index) => (
                    <li
                      key={`${finding.message}:${index}`}
                      className={
                        finding.severity === "error"
                          ? "text-destructive"
                          : "text-amber-700 dark:text-amber-300"
                      }
                    >
                      {finding.message}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ))}
        </div>
      </section>
      <section>
        <h3 className="mb-2 font-medium">Runtime quality checks</h3>
        {runtimeChecks.length === 0 ? (
          <p className="text-muted-foreground">No runtime checks are planned.</p>
        ) : (
          <div className="divide-y border-y">
            {runtimeChecks.map(({ asset, stage }, index) => (
              <div
                key={`${asset}:${stage.label ?? stage.kind}:${index}`}
                className="flex items-center justify-between gap-3 py-2.5"
              >
                <div className="min-w-0">
                  <div className="truncate font-medium">{stage.label || "Quality check"}</div>
                  <div className="truncate font-mono text-[11px] text-muted-foreground">
                    {asset}
                  </div>
                </div>
                <Badge variant={stage.status === "ok" ? "outline" : "destructive"} size="xs">
                  {stage.fidelity}
                </Badge>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function PlanExecution({
  plan,
  contentLoading,
  contentLoaded,
}: {
  plan: PipelinePlan;
  contentLoading: boolean;
  contentLoaded: boolean;
}) {
  const operations = useMemo(
    () =>
      plan.assets.flatMap((asset) =>
        asset.renders.flatMap((render, renderIndex) =>
          render.stages.map((stage, stageIndex) => ({
            key: `${asset.id}:${renderIndex}:${stageIndex}`,
            asset: asset.name,
            render,
            stage,
          })),
        ),
      ),
    [plan],
  );
  const [selectedKey, setSelectedKey] = useState(operations[0]?.key ?? "");
  useEffect(() => {
    if (!operations.some((operation) => operation.key === selectedKey)) {
      setSelectedKey(operations[0]?.key ?? "");
    }
  }, [operations, selectedKey]);
  const operation = operations.find((candidate) => candidate.key === selectedKey) ?? operations[0];

  if (contentLoading && !contentLoaded) {
    return (
      <div className="flex h-80 items-center justify-center gap-2 text-muted-foreground">
        <Loader2 className="size-4 animate-spin" /> Loading rendered operations…
      </div>
    );
  }
  if (!operation) {
    return <p className="text-muted-foreground">No renderable operations are planned.</p>;
  }
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end gap-2">
        <div className="min-w-0 flex-1">
          <Label
            htmlFor="pipeline-plan-operation"
            className="mb-1 block text-[11px] text-muted-foreground"
          >
            Operation
          </Label>
          <Select value={operation.key} onValueChange={setSelectedKey}>
            <SelectTrigger id="pipeline-plan-operation" size="sm" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {operations.map((candidate) => (
                <SelectItem key={candidate.key} value={candidate.key}>
                  {candidate.asset} · {assetRenderStageLabel(candidate.stage)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Badge variant="outline" size="xs">
          {operation.stage.fidelity}
        </Badge>
        <Badge variant="muted" size="xs">
          Preview — not executed
        </Badge>
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
        <span className="font-mono text-foreground">{operation.asset}</span>
        <span>{formatPlanWindow(operation.render.start_date, operation.render.end_date)}</span>
        <span>{operation.stage.language}</span>
      </div>
      <div className="h-[min(52vh,32rem)] min-h-72 overflow-hidden rounded-md border bg-background">
        {operation.stage.content ? (
          <ReadOnlyRenderedOperation
            content={operation.stage.content}
            language={operation.stage.language || "text"}
            modelKey={`pipeline-plan:${plan.id}:${operation.key}`}
          />
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center text-muted-foreground">
            <FileCode2 className="size-5" />
            <p>{operation.stage.message || "This operation is only available at runtime."}</p>
          </div>
        )}
      </div>
    </div>
  );
}

function PlanLoading() {
  return (
    <div className="space-y-4" aria-label="Planning pipeline">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton key={index} className="h-14" />
        ))}
      </div>
      <Skeleton className="h-20" />
      <Skeleton className="h-32" />
    </div>
  );
}

function sourceInputLabel(source?: PipelineRunSource | null) {
  if (!source) return "Policy default";
  return source.source === "working_tree"
    ? "Saved working tree"
    : `Deployment ${source.snapshot_version_id.slice(0, 8)}`;
}

function planSourceLabel(plan: PipelinePlan) {
  return plan.source.kind === "working_tree"
    ? `Saved working tree · ${plan.source.merkle_root.slice(0, 8)}`
    : `Deployment ${plan.source.version_id?.slice(0, 8) || plan.source.merkle_root.slice(0, 8)}`;
}

function runSourceLabel(plan: PipelinePlan) {
  return plan.source.kind === "working_tree"
    ? "working tree"
    : `deployment ${plan.source.version_id?.slice(0, 8) || plan.source.merkle_root.slice(0, 8)}`;
}

function formatPlanWindow(start: string, end: string) {
  const format = (value: string) => {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toISOString().slice(0, 16).replace("T", " ");
  };
  return `${format(start)}–${format(end)} UTC`;
}
