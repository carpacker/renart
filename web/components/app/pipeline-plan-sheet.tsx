"use client";

import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  CheckCircle2,
  CircleAlert,
  FileCode2,
  Loader2,
  Package,
  Play,
  RefreshCw,
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
import { Checkbox } from "@/components/ui/checkbox";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectGroup,
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
  getDeploymentFileDiff,
  getDeployStatus,
  type DeploymentFileDiff,
  type DeployResponse,
  type DeployStatus,
} from "@/lib/api-deploy";
import {
  getEnvSchedules,
  promoteEnvSchedules,
  type EnvSchedule,
  type SchedulerOwnership,
} from "@/lib/api-env-schedules";
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
import { deploymentLabel } from "@/lib/deployment-label";

type PlanSelectionMode = "all" | "needed" | "selector" | "selector_needed";
type SensorMode = "once" | "wait" | "skip";
type PlanIntent = "run" | "deploy";

export function PipelinePlanSheet({
  open,
  onOpenChange,
  pipelineId,
  pipelineName,
  environment,
  timeWindow,
  source,
  intent = "run",
  confirmDestructive = false,
  onAccepted,
  onDeploy,
  onSchedulesChanged,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pipelineId: string;
  pipelineName: string;
  environment: string;
  timeWindow?: { start: string; end: string } | null;
  source?: PipelineRunSource | null;
  intent?: PlanIntent;
  confirmDestructive?: boolean;
  onAccepted?: (run: PipelineRun, plan: PipelinePlan) => void;
  onDeploy?: (expectedSourceMerkle: string) => Promise<DeployResponse>;
  onSchedulesChanged?: () => void | Promise<void>;
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
  const [deployStatus, setDeployStatus] = useState<DeployStatus | null>(null);
  const [deployment, setDeployment] = useState<DeployResponse | null>(null);
  const [schedules, setSchedules] = useState<EnvSchedule[]>([]);
  const [schedulerOwnership, setSchedulerOwnership] = useState<SchedulerOwnership | null>(null);
  const [selectedScheduleKeys, setSelectedScheduleKeys] = useState<Set<string>>(() => new Set());
  const [promoting, setPromoting] = useState(false);
  const [promotionError, setPromotionError] = useState<string | null>(null);
  const [selectorDraft, setSelectorDraft] = useState("*");
  const requestSerial = useRef(0);
  const initialPlanContext = useRef<string | null>(null);
  const requestedSourceKind = intent === "deploy" ? "working_tree" : source?.source;
  const requestedSourceVersion =
    intent !== "deploy" && source?.source === "snapshot" ? source.snapshot_version_id : undefined;

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
        if (next.selection.mode === "selector" || next.selection.mode === "selector_needed") {
          setSelectorDraft(next.selection.selector ?? "");
        }
        setRequest({
          ...canonicalPipelinePlanRequest(next, false),
          purpose: input.purpose,
        });
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
    setDeployStatus(null);
    setDeployment(null);
    setSchedules([]);
    setSchedulerOwnership(null);
    setSelectedScheduleKeys(new Set());
    setPromotionError(null);
    setSelectorDraft("*");
    setLoading(true);
    const serial = ++requestSerial.current;
    void (async () => {
      try {
        await awaitWorkspaceSaves();
        if (serial !== requestSerial.current) return;
        const input: PipelinePlanRequest = {
          purpose: intent === "deploy" ? "deployment" : "execution",
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
        if (intent === "deploy") {
          const [statusResponse, scheduleResponse] = await Promise.all([
            getDeployStatus(pipelineId),
            getEnvSchedules(),
          ]);
          if (serial !== requestSerial.current) return;
          setDeployStatus(statusResponse);
          setSchedules(scheduleResponse.schedules ?? []);
          setSchedulerOwnership(scheduleResponse.scheduler);
        }
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
    intent,
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
  const selectorMode = selectionMode === "selector" || selectionMode === "selector_needed";
  const appliedSelector = request?.selection?.selector?.trim() ?? "";
  const selectorDraftApplied = !selectorMode || selectorDraft.trim() === appliedSelector;
  const selectorPlanIsCurrent = Boolean(
    selectorMode &&
    plan?.selection.mode === selectionMode &&
    plan.selection.selector === appliedSelector,
  );
  const sensorMode = (request?.sensor_mode ?? plan?.context.sensor_mode ?? "once") as SensorMode;
  const fullRefresh = Boolean(request?.full_refresh ?? plan?.context.requested_full_refresh);
  const destructiveConfirmationRequired = Boolean(
    intent === "run" && confirmDestructive && plan?.context.destructive,
  );
  const confirmationMatches =
    !destructiveConfirmationRequired || confirmation.trim() === plan?.context.environment;
  const hasBlockers = Boolean(
    plan && (plan.status === "blocked" || plan.readiness.blockers.length),
  );
  const canConfirm = Boolean(
    plan &&
    !hasBlockers &&
    confirmationMatches &&
    selectorDraftApplied &&
    !loading &&
    !error &&
    !deployment &&
    (intent === "deploy" ? plan.summary.assets > 0 : plan.summary.execution_units > 0),
  );

  const applySelector = () => {
    const selector = selectorDraft.trim();
    if (!selector || !selectorMode) return;
    updateRequest((current) => ({
      ...current,
      selection: { mode: selectionMode, selector },
    }));
  };

  const pipelineSchedules = useMemo(
    () =>
      plan
        ? schedules.filter(
            (schedule) =>
              schedule.pipeline_uuid === plan.pipeline_uuid && schedule.status !== "archived",
          )
        : [],
    [plan, schedules],
  );
  const promotionCandidates = useMemo(
    () =>
      deployment
        ? pipelineSchedules.filter(
            (schedule) => schedule.snapshot_version_id !== deployment.snapshot.version_id,
          )
        : [],
    [deployment, pipelineSchedules],
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
      if (intent === "deploy") {
        if (!onDeploy) {
          throw new Error("Deployment is unavailable.");
        }
        const response = await onDeploy(plan.source.merkle_root);
        setDeployment(response);
        setDeployStatus(await getDeployStatus(pipelineId));
        const scheduleResponse = await getEnvSchedules();
        setSchedules(scheduleResponse.schedules ?? []);
        setSchedulerOwnership(scheduleResponse.scheduler);
        setSelectedScheduleKeys(new Set());
        setActiveTab("schedules");
        return;
      }
      const response = await confirmPipelinePlan(pipelineId, {
        plan_id: plan.id,
        plan: canonicalPipelinePlanRequest(plan, false),
        reviewed: canonicalPipelinePlanReviewedIdentity(plan),
        confirmed_environment: destructiveConfirmationRequired ? confirmation.trim() : undefined,
      });
      onAccepted?.(response.run, plan);
      onOpenChange(false);
    } catch (cause) {
      if (
        intent === "deploy" &&
        cause instanceof APIError &&
        cause.code === "deployment_source_changed" &&
        request
      ) {
        setError("The saved source changed after review. Review the refreshed deployment plan.");
        setDeployStatus(await getDeployStatus(pipelineId));
        await fetchPlan(request);
        return;
      }
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

  const promoteSelectedSchedules = async () => {
    if (!deployment || !plan || selectedScheduleKeys.size === 0) return;
    const selected = promotionCandidates.filter((schedule) =>
      selectedScheduleKeys.has(schedule.environment),
    );
    setPromoting(true);
    setPromotionError(null);
    try {
      await promoteEnvSchedules(
        pipelineId,
        deployment.snapshot.version_id,
        selected.map((schedule) => ({
          environment: schedule.environment,
          expected_snapshot_version_id: schedule.snapshot_version_id ?? "",
        })),
      );
      const scheduleResponse = await getEnvSchedules();
      setSchedules(scheduleResponse.schedules ?? []);
      setSchedulerOwnership(scheduleResponse.scheduler);
      setSelectedScheduleKeys(new Set());
      await onSchedulesChanged?.();
    } catch (cause) {
      setPromotionError(cause instanceof Error ? cause.message : "Schedules could not be updated.");
    } finally {
      setPromoting(false);
    }
  };

  const sourceLabel = plan
    ? planSourceLabel(plan)
    : intent === "deploy"
      ? "Saved working tree"
      : sourceInputLabel(source);
  const finalActionLabel = plan
    ? intent === "deploy"
      ? `Deploy ${plan.summary.assets} ${plan.summary.assets === 1 ? "asset" : "assets"}`
      : `Run ${plan.summary.assets} ${plan.summary.assets === 1 ? "asset" : "assets"} from ${runSourceLabel(plan)}`
    : intent === "deploy"
      ? "Deploy pipeline"
      : "Run pipeline";

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="w-full gap-0 p-0 sm:max-w-2xl lg:max-w-3xl"
        data-testid="pipeline-plan-sheet"
      >
        <SheetHeader className="shrink-0 border-b px-5 py-4 pr-12">
          <div className="flex min-w-0 items-center gap-2">
            <SheetTitle className="truncate">
              {intent === "deploy" ? "Review deployment" : "Review pipeline run"}
            </SheetTitle>
            {plan ? <PlanStatusBadge status={plan.status} /> : null}
            {(loading || contentLoading) && plan ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : null}
          </div>
          <SheetDescription>
            {pipelineName} · saved source preview ·{" "}
            {intent === "deploy"
              ? "no data is executed by deployment"
              : "nothing executes until you confirm"}
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
          {intent === "run" ? (
            <>
              <FieldGroup className="mt-3 flex-row flex-wrap items-end gap-3 border-t pt-3">
                <Field className="min-w-40 flex-1 gap-1 sm:max-w-52">
                  <FieldLabel
                    htmlFor="pipeline-plan-scope"
                    className="text-[11px] text-muted-foreground"
                  >
                    Scope
                  </FieldLabel>
                  <Select
                    value={selectionMode}
                    onValueChange={(value) => {
                      const mode = value as PlanSelectionMode;
                      const usesSelector = mode === "selector" || mode === "selector_needed";
                      const selector = selectorDraft.trim() || appliedSelector || "*";
                      if (usesSelector) setSelectorDraft(selector);
                      updateRequest((current) => ({
                        ...current,
                        selection: usesSelector ? { mode, selector } : { mode },
                      }));
                    }}
                  >
                    <SelectTrigger id="pipeline-plan-scope" size="sm" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="all">Entire pipeline</SelectItem>
                        <SelectItem value="needed">Needed assets</SelectItem>
                        <SelectItem value="selector">Matching selector</SelectItem>
                        <SelectItem value="selector_needed">Needed matching selector</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field className="min-w-36 flex-1 gap-1 sm:max-w-44">
                  <FieldLabel
                    htmlFor="pipeline-plan-sensor"
                    className="text-[11px] text-muted-foreground"
                  >
                    Sensors
                  </FieldLabel>
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
                      <SelectGroup>
                        <SelectItem value="once">Check once</SelectItem>
                        <SelectItem value="wait">Wait</SelectItem>
                        <SelectItem value="skip">Skip</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field
                  orientation="horizontal"
                  className="h-8 w-auto rounded-md border px-3 text-xs"
                >
                  <Switch
                    id="pipeline-plan-full-refresh"
                    size="sm"
                    checked={fullRefresh}
                    onCheckedChange={(checked) =>
                      updateRequest((current) => ({ ...current, full_refresh: checked }))
                    }
                  />
                  <FieldLabel htmlFor="pipeline-plan-full-refresh" className="font-normal">
                    Full refresh
                  </FieldLabel>
                </Field>
              </FieldGroup>
              {selectorMode ? (
                <Field
                  className="mt-3 max-w-3xl border-t pt-3"
                  data-invalid={!selectorDraft.trim()}
                >
                  <FieldLabel htmlFor="pipeline-plan-selector">Asset selector</FieldLabel>
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <Input
                      id="pipeline-plan-selector"
                      value={selectorDraft}
                      onChange={(event) => setSelectorDraft(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          applySelector();
                        }
                      }}
                      placeholder="tag:daily,path:assets/marts +analytics.orders"
                      aria-invalid={!selectorDraft.trim()}
                      className="min-w-56 flex-1 font-mono"
                    />
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={applySelector}
                      disabled={!selectorDraft.trim() || selectorDraftApplied || loading}
                    >
                      Apply
                    </Button>
                  </div>
                  <FieldDescription>
                    {!selectorDraftApplied
                      ? "Apply the expression to validate it and preview its assets."
                      : loading
                        ? "Resolving selector…"
                        : selectorPlanIsCurrent && plan
                          ? `${plan.summary.assets} ${plan.summary.assets === 1 ? "asset" : "assets"} selected. Use spaces for union, commas for intersection, and + for graph expansion.`
                          : "Use spaces for union, commas for intersection, and + for graph expansion."}
                  </FieldDescription>
                </Field>
              ) : null}
            </>
          ) : (
            <Alert className="mt-3">
              <Package />
              <AlertTitle>Representative execution preview</AlertTitle>
              <AlertDescription>
                Deployment stores this saved source, not rendered SQL. Scheduled runs render it
                again with their actual interval, environment, and variables.
              </AlertDescription>
            </Alert>
          )}
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
            <ScrollArea className="min-w-0 shrink-0 border-b" viewportClassName="px-5 py-2">
              <TabsList variant="line" className="w-max min-w-full justify-start">
                <TabsTrigger value="summary">Summary</TabsTrigger>
                <TabsTrigger value="assets">Assets</TabsTrigger>
                <TabsTrigger value="checks">Checks</TabsTrigger>
                <TabsTrigger value="execution">Execution</TabsTrigger>
                {intent === "deploy" ? <TabsTrigger value="changes">Files</TabsTrigger> : null}
                {intent === "deploy" ? (
                  <TabsTrigger value="schedules">Schedules</TabsTrigger>
                ) : null}
              </TabsList>
            </ScrollArea>
            <ScrollArea className="min-h-0 flex-1">
              <TabsContent value="summary" className="m-0 space-y-4 p-5">
                <PlanSummary plan={plan} intent={intent} deployment={deployment} />
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
              {intent === "deploy" ? (
                <TabsContent value="changes" className="m-0 p-5">
                  <DeploymentFileChanges pipelineId={pipelineId} status={deployStatus} />
                </TabsContent>
              ) : null}
              {intent === "deploy" ? (
                <TabsContent value="schedules" className="m-0 p-5">
                  <DeploymentSchedulePromotion
                    schedules={pipelineSchedules}
                    candidates={promotionCandidates}
                    deployment={deployment}
                    ownership={schedulerOwnership}
                    selected={selectedScheduleKeys}
                    onSelectedChange={setSelectedScheduleKeys}
                    error={promotionError}
                  />
                </TabsContent>
              ) : null}
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
          {deployment ? (
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="min-w-0 flex-1 text-left text-[11px] text-muted-foreground">
                {promotionCandidates.length > 0
                  ? `${promotionCandidates.length} schedule${promotionCandidates.length === 1 ? " is" : "s are"} still pinned to an older deployment. Only selected schedules will move.`
                  : "Every schedule for this pipeline is already on this deployment."}
              </div>
              <div className="flex items-center gap-2">
                <Button variant="outline" onClick={() => onOpenChange(false)}>
                  Close
                </Button>
                {promotionCandidates.length > 0 ? (
                  <Button
                    onClick={() => void promoteSelectedSchedules()}
                    disabled={
                      selectedScheduleKeys.size === 0 ||
                      promoting ||
                      schedulerOwnership?.state !== "owner"
                    }
                  >
                    {promoting ? (
                      <Loader2 data-icon="inline-start" className="animate-spin" />
                    ) : (
                      <RefreshCw data-icon="inline-start" />
                    )}
                    {promoting
                      ? "Updating…"
                      : `Update ${selectedScheduleKeys.size} schedule${selectedScheduleKeys.size === 1 ? "" : "s"}`}
                  </Button>
                ) : null}
              </div>
            </div>
          ) : (
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="min-w-0 flex-1 text-left text-[11px] text-muted-foreground">
                {intent === "deploy"
                  ? "Deployment rechecks the saved source identity. It never deploys an unsaved editor buffer."
                  : selectionMode === "needed" || selectionMode === "selector_needed"
                    ? "Confirmation omits work that became fresh, but never adds new work without another review."
                    : plan?.readiness.active_run_id
                      ? "Another execution owns a write resource needed by this plan."
                      : "Confirmation rechecks the complete plan before the run is admitted."}
              </div>
              <Button onClick={() => void confirm()} disabled={!canConfirm || confirming}>
                {confirming ? (
                  <Loader2 data-icon="inline-start" className="animate-spin" />
                ) : intent === "deploy" ? (
                  <Package data-icon="inline-start" />
                ) : (
                  <Play data-icon="inline-start" />
                )}
                {confirming ? (intent === "deploy" ? "Deploying…" : "Starting…") : finalActionLabel}
              </Button>
            </div>
          )}
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

function PlanSummary({
  plan,
  intent,
  deployment,
}: {
  plan: PipelinePlan;
  intent: PlanIntent;
  deployment: DeployResponse | null;
}) {
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
      {deployment ? (
        <Alert>
          <CheckCircle2 />
          <AlertTitle>
            {deployment.created ? "Deployment created" : "Deployment already current"}
          </AlertTitle>
          <AlertDescription>
            {deploymentLabel(deployment.snapshot.ordinal, deployment.snapshot.version_id)} ·{" "}
            {deployment.snapshot.file_count} files · source{" "}
            {deployment.snapshot.merkle_root.slice(0, 8)}
          </AlertDescription>
        </Alert>
      ) : null}
      {!deployment &&
      plan.readiness.blockers.length === 0 &&
      plan.readiness.warnings.length === 0 ? (
        <Alert>
          <CheckCircle2 />
          <AlertTitle>{intent === "deploy" ? "Ready to deploy" : "Ready to run"}</AlertTitle>
          <AlertDescription>
            The saved source and complete operation graph passed planning.
          </AlertDescription>
        </Alert>
      ) : null}
      {intent === "run" && plan.readiness.active_run_id ? (
        <Alert variant="destructive">
          <ShieldAlert />
          <AlertTitle>Conflicting run</AlertTitle>
          <AlertDescription>
            Another queued or running execution owns a selected write resource.{" "}
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

type DeploymentChange = {
  path: string;
  label: "Added" | "Changed" | "Removed";
  variant: "secondary" | "outline" | "destructive";
};

function DeploymentFileChanges({
  pipelineId,
  status,
}: {
  pipelineId: string;
  status: DeployStatus | null;
}) {
  const changes = useMemo<DeploymentChange[]>(
    () =>
      status
        ? [
            ...(status.added_files ?? []).map((path) => ({
              path,
              label: "Added" as const,
              variant: "secondary" as const,
            })),
            ...(status.changed_files ?? []).map((path) => ({
              path,
              label: "Changed" as const,
              variant: "outline" as const,
            })),
            ...(status.removed_files ?? []).map((path) => ({
              path,
              label: "Removed" as const,
              variant: "destructive" as const,
            })),
          ]
        : [],
    [status],
  );
  const [selectedPath, setSelectedPath] = useState("");
  const [diff, setDiff] = useState<DeploymentFileDiff | null>(null);
  const [diffLoading, setDiffLoading] = useState(false);
  const [diffError, setDiffError] = useState<string | null>(null);

  useEffect(() => {
    setSelectedPath((current) =>
      changes.some((change) => change.path === current) ? current : (changes[0]?.path ?? ""),
    );
  }, [changes]);

  useEffect(() => {
    if (!status || !selectedPath) {
      setDiff(null);
      setDiffError(null);
      setDiffLoading(false);
      return;
    }
    let cancelled = false;
    setDiffLoading(true);
    setDiffError(null);
    getDeploymentFileDiff(pipelineId, selectedPath, status.version_id)
      .then((nextDiff) => {
        if (!cancelled) setDiff(nextDiff);
      })
      .catch((cause: unknown) => {
        if (cancelled) return;
        setDiff(null);
        setDiffError(
          cause instanceof Error ? cause.message : "Could not load the file comparison.",
        );
      })
      .finally(() => {
        if (!cancelled) setDiffLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [pipelineId, selectedPath, status?.source_merkle, status?.version_id]);

  if (!status) {
    return <Skeleton className="h-40" />;
  }
  const groups = [
    { label: "Added", paths: status.added_files ?? [], variant: "secondary" as const },
    { label: "Changed", paths: status.changed_files ?? [], variant: "outline" as const },
    { label: "Removed", paths: status.removed_files ?? [], variant: "destructive" as const },
  ].filter((group) => group.paths.length > 0);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <Badge variant="muted" size="xs" title={status.source_merkle}>
          Saved source {status.source_merkle?.slice(0, 8) || "unavailable"}
        </Badge>
        {status.has_snapshot ? (
          <span>
            Compared with {deploymentLabel(status.ordinal, status.version_id, "deployment")}
          </span>
        ) : (
          <span>First deployment; every source file is new.</span>
        )}
      </div>
      {groups.length === 0 ? (
        <Alert>
          <CheckCircle2 />
          <AlertTitle>No source changes</AlertTitle>
          <AlertDescription>
            The saved working tree already matches the latest deployment.
          </AlertDescription>
        </Alert>
      ) : (
        <div className="grid min-w-0 gap-4 xl:grid-cols-[16rem_minmax(0,1fr)]">
          <div className="space-y-4">
            {groups.map((group) => (
              <section key={group.label} className="space-y-2">
                <h3 className="text-xs font-medium">
                  {group.label}{" "}
                  <span className="text-muted-foreground">({group.paths.length})</span>
                </h3>
                <ul className="divide-y overflow-hidden rounded-md border">
                  {group.paths.map((path) => (
                    <li key={path}>
                      <button
                        type="button"
                        aria-pressed={selectedPath === path}
                        className={cn(
                          "flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left text-xs transition-colors hover:bg-muted/50",
                          selectedPath === path && "bg-muted",
                        )}
                        onClick={() => setSelectedPath(path)}
                      >
                        <Badge variant={group.variant} size="xs">
                          {group.label}
                        </Badge>
                        <span className="min-w-0 truncate font-mono" title={path}>
                          {path}
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            ))}
          </div>
          <DeploymentFileDiffPreview
            pipelineId={pipelineId}
            sourceVersion={status.version_id}
            path={selectedPath}
            diff={diff}
            loading={diffLoading}
            error={diffError}
          />
        </div>
      )}
    </div>
  );
}

function DeploymentFileDiffPreview({
  pipelineId,
  sourceVersion,
  path,
  diff,
  loading,
  error,
}: {
  pipelineId: string;
  sourceVersion?: string;
  path: string;
  diff: DeploymentFileDiff | null;
  loading: boolean;
  error: string | null;
}) {
  if (loading && !diff) {
    return <Skeleton className="h-80 min-w-0" />;
  }
  if (error) {
    return (
      <Alert variant="destructive" className="min-w-0">
        <AlertTriangle />
        <AlertTitle>Could not load this comparison</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }
  if (!diff) return null;
  if (diff.binary || diff.too_large) {
    return (
      <Alert className="min-w-0">
        <FileCode2 />
        <AlertTitle>{diff.binary ? "Binary file" : "File is too large to preview"}</AlertTitle>
        <AlertDescription>
          {path} is included in the deployment comparison, but its contents are not sent to the
          browser.
        </AlertDescription>
      </Alert>
    );
  }

  const language = deploymentFileLanguage(path);
  const modelPrefix = `deployment-diff:${pipelineId}:${sourceVersion ?? "first"}:${path}`;
  return (
    <div className="grid min-w-0 gap-3 lg:grid-cols-2">
      <DeploymentFileVersion
        title="Current deployment"
        exists={diff.before_exists}
        content={diff.before ?? ""}
        language={language}
        modelKey={`${modelPrefix}:before`}
      />
      <DeploymentFileVersion
        title="Saved workspace"
        exists={diff.after_exists}
        content={diff.after ?? ""}
        language={language}
        modelKey={`${modelPrefix}:after`}
      />
    </div>
  );
}

function DeploymentFileVersion({
  title,
  exists,
  content,
  language,
  modelKey,
}: {
  title: string;
  exists: boolean;
  content: string;
  language: string;
  modelKey: string;
}) {
  return (
    <section className="min-w-0 overflow-hidden rounded-md border">
      <div className="border-b bg-muted/30 px-3 py-2 text-xs font-medium">{title}</div>
      <div className="h-80 min-w-0">
        {exists ? (
          <ReadOnlyRenderedOperation content={content} language={language} modelKey={modelKey} />
        ) : (
          <div className="flex h-full items-center justify-center p-4 text-center text-xs text-muted-foreground">
            File does not exist in this source version.
          </div>
        )}
      </div>
    </section>
  );
}

function deploymentFileLanguage(path: string) {
  const extension = path.split(".").pop()?.toLowerCase();
  switch (extension) {
    case "sql":
      return "sql";
    case "py":
      return "python";
    case "json":
      return "json";
    case "yaml":
    case "yml":
      return "yaml";
    case "md":
      return "markdown";
    default:
      return "text";
  }
}

function DeploymentSchedulePromotion({
  schedules,
  candidates,
  deployment,
  ownership,
  selected,
  onSelectedChange,
  error,
}: {
  schedules: EnvSchedule[];
  candidates: EnvSchedule[];
  deployment: DeployResponse | null;
  ownership: SchedulerOwnership | null;
  selected: Set<string>;
  onSelectedChange: (selected: Set<string>) => void;
  error: string | null;
}) {
  if (schedules.length === 0) {
    return (
      <Alert>
        <CheckCircle2 />
        <AlertTitle>No schedules to update</AlertTitle>
        <AlertDescription>
          This pipeline has no active or paused environment schedules.
        </AlertDescription>
      </Alert>
    );
  }

  if (!deployment) {
    return (
      <div className="space-y-3">
        <div>
          <h3 className="text-sm font-medium">Current schedule pins</h3>
          <p className="text-xs text-muted-foreground">
            Deployment does not move these automatically. After creating the deployment, you can
            select exactly which schedules to promote.
          </p>
        </div>
        <dl className="divide-y rounded-md border">
          {schedules.map((schedule) => (
            <div
              key={schedule.environment}
              className="flex min-w-0 items-center justify-between gap-3 px-3 py-2 text-xs"
            >
              <dt className="min-w-0 truncate font-medium">{schedule.environment}</dt>
              <dd className="shrink-0 font-mono text-muted-foreground">
                {deploymentLabel(schedule.snapshot_ordinal, schedule.snapshot_version_id)}
              </dd>
            </div>
          ))}
        </dl>
      </div>
    );
  }

  if (candidates.length === 0) {
    return (
      <Alert>
        <CheckCircle2 />
        <AlertTitle>Schedules are current</AlertTitle>
        <AlertDescription>
          Every schedule for this pipeline is pinned to{" "}
          {deploymentLabel(
            deployment.snapshot.ordinal,
            deployment.snapshot.version_id,
            "deployment",
          )}
          .
        </AlertDescription>
      </Alert>
    );
  }

  const canPromote = ownership?.state === "owner";
  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-medium">Promote selected schedules</h3>
        <p className="text-xs text-muted-foreground">
          Move only the checked schedule pins to{" "}
          {deploymentLabel(
            deployment.snapshot.ordinal,
            deployment.snapshot.version_id,
            "deployment",
          )}
          . Unchecked schedules keep running their current deployment.
        </p>
      </div>
      {!canPromote ? (
        <Alert variant="destructive">
          <ShieldAlert />
          <AlertTitle>Schedules are read-only here</AlertTitle>
          <AlertDescription>
            {ownership?.message ?? "Scheduler ownership is unavailable."}
          </AlertDescription>
        </Alert>
      ) : null}
      {error ? (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>Schedules were not updated</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <FieldGroup data-slot="checkbox-group" className="gap-2">
        {candidates.map((schedule) => {
          const checkboxID = `promote-schedule-${schedule.environment}`;
          return (
            <Field
              key={schedule.environment}
              orientation="horizontal"
              data-disabled={!canPromote || undefined}
              className="rounded-md border px-3 py-2"
            >
              <Checkbox
                id={checkboxID}
                checked={selected.has(schedule.environment)}
                disabled={!canPromote}
                onCheckedChange={(checked) => {
                  const next = new Set(selected);
                  if (checked === true) next.add(schedule.environment);
                  else next.delete(schedule.environment);
                  onSelectedChange(next);
                }}
              />
              <FieldContent>
                <FieldLabel htmlFor={checkboxID}>{schedule.environment}</FieldLabel>
                <FieldDescription>
                  {deploymentLabel(schedule.snapshot_ordinal, schedule.snapshot_version_id)} ·{" "}
                  {schedule.status}
                </FieldDescription>
              </FieldContent>
            </Field>
          );
        })}
      </FieldGroup>
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
    : deploymentLabel(undefined, source.snapshot_version_id);
}

function planSourceLabel(plan: PipelinePlan) {
  return plan.source.kind === "working_tree"
    ? `Saved working tree · ${plan.source.merkle_root.slice(0, 8)}`
    : deploymentLabel(
        plan.source.deployment_ordinal,
        plan.source.version_id || plan.source.merkle_root,
      );
}

function runSourceLabel(plan: PipelinePlan) {
  return plan.source.kind === "working_tree"
    ? "working tree"
    : deploymentLabel(
        plan.source.deployment_ordinal,
        plan.source.version_id || plan.source.merkle_root,
        "deployment",
      );
}

function formatPlanWindow(start: string, end: string) {
  const format = (value: string) => {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toISOString().slice(0, 16).replace("T", " ");
  };
  return `${format(start)}–${format(end)} UTC`;
}
