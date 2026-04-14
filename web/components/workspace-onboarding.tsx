"use client";

import { DuckDBOnboardingForm } from "@/components/onboarding/duckdb-onboarding-form";
import { GenericOnboardingForm } from "@/components/onboarding/generic-onboarding-form";
import { OnboardingConnectionIcon } from "@/components/onboarding/connection-icons";
import { PostgresOnboardingForm } from "@/components/onboarding/postgres-onboarding-form";
import { useOnboardingFlow } from "@/hooks/use-onboarding-flow";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  OnboardingSessionState,
  WorkspaceConfigResponse,
} from "@/lib/types";

type WorkspaceOnboardingProps = {
  workspaceConfig: WorkspaceConfigResponse;
  onboardingState: OnboardingSessionState;
  onCreateConnection: (input: {
    environment_name: string;
    name: string;
    type: string;
    values: Record<string, unknown>;
  }) => Promise<WorkspaceConfigResponse>;
  onUpdateConnection: (input: {
    environment_name: string;
    current_name?: string;
    name: string;
    type: string;
    values: Record<string, unknown>;
  }) => Promise<WorkspaceConfigResponse>;
  onReloadConfig: () => Promise<void> | void;
  onReloadWorkspace?: () => Promise<void> | void;
};

const TYPE_LABELS: Record<string, string> = {
  postgres: "Postgres",
  duckdb: "DuckDB",
  snowflake: "Snowflake",
  google_cloud_platform: "BigQuery",
  redshift: "Redshift",
  databricks: "Databricks",
};

export function WorkspaceOnboarding({
  workspaceConfig,
  onboardingState,
  onCreateConnection,
  onUpdateConnection,
  onReloadConfig,
  onReloadWorkspace,
}: WorkspaceOnboardingProps) {
  const {
    connectionName,
    defaultDraftValues,
    defaultEnvironment,
    discoveryBusy,
    discoveryError,
    discoveryState,
    draftValues,
    featuredTypes,
    busy,
    importDisabled,
    importForm,
    importResult,
    navigateToStep,
    handleComplete,
    handleSaveAndImport,
    handleSkip,
    handleSelectDatabase,
    runDiscovery,
    selectedTables,
    selectedType,
    updateImportFormField,
    updateSelectedTables,
    chooseType,
    step,
  } = useOnboardingFlow({
    workspaceConfig,
    onboardingState,
    onCreateConnection,
    onUpdateConnection,
    onReloadConfig,
    onReloadWorkspace,
  });

  return (
    <div data-testid="workspace-onboarding" className="flex min-h-screen flex-col bg-background">
      <div className="mx-auto flex w-full max-w-5xl flex-1 flex-col px-6 py-8">
        <div className="mb-8 flex items-start justify-between gap-4">
          <div>
            <div className="text-sm font-medium text-muted-foreground">Welcome to Bruin Web</div>
            <h1 className="mt-1 text-3xl font-semibold tracking-tight">
              Set up your first connection and import assets
            </h1>
            <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
              Pick a warehouse, validate access, choose the database or file you want, and import the tables into a pipeline.
            </p>
          </div>
          <Button variant="ghost" onClick={() => void handleSkip()}>
            Skip for now
          </Button>
        </div>

        <div className="mb-6 flex gap-2 text-xs text-muted-foreground">
          <StepPill active={step === "connection-type"}>1. Choose warehouse</StepPill>
          <StepPill active={step === "connection-config"}>2. Validate access</StepPill>
          <StepPill active={step === "import"}>3. Choose database and import</StepPill>
          <StepPill active={step === "success"}>4. Done</StepPill>
        </div>

        {step === "connection-type" ? (
          <div data-testid="onboarding-step-connection-type" className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {featuredTypes.map((connectionType) => (
              <button
                key={connectionType.type_name}
                type="button"
                onClick={() => void chooseType(connectionType.type_name)}
                className="rounded-2xl border bg-card p-5 text-left transition hover:border-primary/60 hover:bg-muted/20"
              >
                <div className="flex items-center gap-3">
                  <span className="flex size-10 items-center justify-center rounded-xl border bg-background">
                    <OnboardingConnectionIcon type={connectionType.type_name} />
                  </span>
                  <div className="text-lg font-medium">{TYPE_LABELS[connectionType.type_name] ?? connectionType.type_name}</div>
                </div>
                <div className="mt-3 text-sm text-muted-foreground">
                  Connect {TYPE_LABELS[connectionType.type_name] ?? connectionType.type_name} and import existing assets.
                </div>
              </button>
            ))}
          </div>
        ) : null}

        {step === "connection-config" ? (
          <div data-testid="onboarding-step-connection-config" className="max-w-3xl space-y-4">
            {selectedType === "postgres" ? (
              <PostgresOnboardingForm
                busy={discoveryBusy}
                defaultName={connectionName}
                defaultValues={defaultDraftValues}
                environmentName={defaultEnvironment}
                initialValues={draftValues}
                onSubmit={async (values) => {
                  await runDiscovery(values);
                }}
              />
            ) : selectedType === "duckdb" ? (
              <DuckDBOnboardingForm
                busy={discoveryBusy}
                defaultName={connectionName}
                defaultValues={defaultDraftValues}
                environmentName={defaultEnvironment}
                initialValues={draftValues}
                onSubmit={async (values) => {
                  await runDiscovery(values, String(values.path ?? ""));
                }}
              />
            ) : (
              <GenericOnboardingForm
                busy={discoveryBusy}
                defaultName={connectionName}
                defaultValues={defaultDraftValues}
                environmentName={defaultEnvironment}
                initialValues={draftValues}
                onSubmit={async (values) => {
                  await runDiscovery(values);
                }}
              />
            )}
            {discoveryError ? (
              <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {discoveryError}
              </div>
            ) : null}
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => void navigateToStep("connection-type")}>Back</Button>
            </div>
          </div>
        ) : null}

        {step === "import" ? (
          <Card className="max-w-2xl rounded-2xl border shadow-sm" data-testid="onboarding-step-import">
            <CardHeader className="border-b px-6 py-5">
              <CardTitle className="text-xl font-semibold tracking-tight">Choose database and import</CardTitle>
              <CardDescription>
                Your validated connection will be saved as `{connectionName}` when you import.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6 px-6 py-5">
              {discoveryError ? (
                <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  {discoveryError}
                </div>
              ) : null}

              {selectedType !== "duckdb" ? (
                <div className="grid gap-1.5">
                  <Label>Database</Label>
                  <div className="flex flex-wrap gap-2">
                    {discoveryState.databases.map((database) => (
                      <Button
                        key={database}
                        type="button"
                        variant={importForm.database === database ? "default" : "outline"}
                        onClick={() => void handleSelectDatabase(database)}
                        disabled={discoveryBusy}
                      >
                        {database}
                      </Button>
                    ))}
                  </div>
                </div>
              ) : (
                <div className="grid gap-1.5">
                  <Label>DuckDB file</Label>
                  <Input value={String(draftValues.path ?? "")} disabled />
                </div>
              )}

              {discoveryState.tables.length > 0 ? (
                <div className="rounded-md border bg-muted/20 p-3">
                  <div className="text-sm font-medium">Discovered tables</div>
                  <div className="mt-2 max-h-56 overflow-auto text-sm">
                    {discoveryState.tables.map((table) => (
                      <label key={table.name} className="flex items-center gap-2 py-1">
                        <Checkbox
                          checked={selectedTables.includes(table.name)}
                          onCheckedChange={(checked) => {
                            const nextSelected = checked
                              ? [...selectedTables, table.name]
                              : selectedTables.filter((item) => item !== table.name);
                            void updateSelectedTables(nextSelected);
                          }}
                        />
                        <span>{table.name}</span>
                      </label>
                    ))}
                  </div>
                </div>
              ) : null}

              <div className="grid gap-4">
                <div className="grid gap-1.5">
                  <Label>Pipeline name</Label>
                  <Input
                    value={importForm.pipelineName}
                    onChange={(event) => {
                      void updateImportFormField("pipelineName", event.target.value);
                    }}
                    placeholder="analytics"
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label>Schema</Label>
                  <Input
                    value={importForm.schema}
                    onChange={(event) => {
                      void updateImportFormField("schema", event.target.value);
                    }}
                    placeholder="public"
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label>Pattern</Label>
                  <Input
                    value={importForm.pattern}
                    onChange={(event) => {
                      void updateImportFormField("pattern", event.target.value);
                    }}
                    placeholder="customer_*"
                  />
                </div>
              </div>

              {importResult?.error ? (
                <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  {importResult.error}
                </div>
              ) : null}
            </CardContent>
            <CardFooter className="flex items-center justify-between gap-2 border-t px-6 py-4">
              <Button variant="outline" onClick={() => void navigateToStep("connection-config")}>Back</Button>
              <Button onClick={() => void handleSaveAndImport()} disabled={importDisabled}>
                Save connection and import
              </Button>
            </CardFooter>
          </Card>
        ) : null}

        {step === "success" ? (
          <Card className="rounded-2xl border shadow-sm" data-testid="onboarding-step-success">
            <CardHeader className="border-b px-6 py-5">
              <CardTitle className="text-xl font-semibold tracking-tight">Workspace is ready</CardTitle>
              <CardDescription>
                Your connection was saved and the selected tables were imported successfully.
              </CardDescription>
            </CardHeader>
            <CardContent className="px-6 py-5">
              {importResult?.output ? (
                <pre className="max-h-64 overflow-auto rounded-md border bg-muted/30 p-3 text-xs">
                  {importResult.output}
                </pre>
              ) : null}
            </CardContent>
            <CardFooter className="flex justify-between gap-2 border-t px-6 py-4">
              <Button variant="outline" onClick={() => void navigateToStep("import")}>Back</Button>
              <Button onClick={() => void handleComplete()}>Open workspace</Button>
            </CardFooter>
          </Card>
        ) : null}
      </div>
    </div>
  );
}

function StepPill({ active, children }: { active: boolean; children: string }) {
  return (
    <div
      className={`rounded-full border px-3 py-1 ${
        active ? "border-primary bg-primary/10 text-primary" : "border-border"
      }`}
    >
      {children}
    </div>
  );
}
