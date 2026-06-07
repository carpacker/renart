import { AlertTriangle, BarChart3, BookOpen, Database, LayoutDashboard, MoreHorizontal, Network, Play, Plus, RotateCw, X } from "lucide-react";
import { ReactNode, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  DelimitedCardContent,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";

import { editorLinesFor, kindMeta } from "./redesign-data";
import { PageHeader, RedesignPage, RedesignPanel, SectionCard, SimpleTable } from "./redesign-primitives";

const notebookNodes = [
  { id: "nb_intro", name: "intro", kind: "md", description: "Title and context", out: null },
  { id: "nb_pull", name: "pull_revenue", kind: "sql", description: "SELECT from revenue_daily", out: "table" },
  { id: "nb_plot", name: "plot_revenue", kind: "python", description: "Plotly line of revenue", out: "chart" },
] as const;

const notebookEdges = [["nb_pull", "nb_plot"]] as const;

export function RedesignNotebookPage({ notebookId }: { notebookId: string }) {
  const [showDependencies, setShowDependencies] = useState(true);
  const [staleCells, setStaleCells] = useState<string[]>(["nb_plot"]);
  const runAll = () => setStaleCells([]);
  const runCell = (cellId: string) => setStaleCells((current) => current.filter((id) => id !== cellId));

  return (
    <RedesignPage>
      <PageHeader
        title={notebookId}
        subtitle="Notebook · exploratory SQL and Python cells"
        actions={(
          <div className="flex items-center gap-2">
            <Button variant={showDependencies ? "secondary" : "outline"} size="sm" className="hidden sm:inline-flex" onClick={() => setShowDependencies((value) => !value)}><Network className="size-3.5" />Dependencies</Button>
            <Button size="sm" onClick={runAll}><Play className="size-3.5" />Run all</Button>
          </div>
        )}
      />
      {staleCells.length > 0 ? (
        <div className="mx-3 mb-2 flex items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
          <AlertTriangle className="size-3.5" />
          <span className="min-w-0 flex-1">{staleCells.length} cell stale because an upstream cell changed.</span>
          <Button size="xs" onClick={runAll}><RotateCw className="size-3" />Recompute stale</Button>
        </div>
      ) : null}
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 px-3 pb-3 xl:grid-cols-[minmax(0,1fr)_260px]">
        <div className="min-h-0 overflow-auto">
          <div className="mx-auto flex max-w-5xl flex-col gap-3">
            {notebookNodes.map((cell) => (
              <NotebookCell key={cell.id} type={cell.kind} title={cell.name} stale={staleCells.includes(cell.id)} onRun={() => runCell(cell.id)}>
                {cell.kind === "md" ? (
                  <div className="space-y-1 text-sm">
                    <h2 className="text-lg font-semibold">Revenue exploration</h2>
                    <p className="text-muted-foreground">Quick look at daily revenue vs. forecast.</p>
                  </div>
                ) : (
                  <CodeBlock lines={editorLinesFor({ kind: cell.kind, name: cell.name })} />
                )}
                {cell.out === "table" ? <div className="mt-3 overflow-hidden rounded-lg border"><SimpleTable columns={["day", "revenue"]} rows={[["2026-06-03", "$4,902"], ["2026-06-02", "$5,118"], ["2026-06-01", "$4,210"]]} /></div> : null}
                {cell.out === "chart" ? <div className="relative mt-3 h-36 rounded-lg border bg-muted/30 p-3"><Sparkline />{staleCells.includes(cell.id) ? <div className="absolute inset-0 flex items-center justify-center rounded-lg bg-background/70"><Button size="sm" onClick={() => runCell(cell.id)}><RotateCw className="size-3.5" />Recompute</Button></div> : null}</div> : null}
              </NotebookCell>
            ))}
            <div className="flex gap-2">
              <Button variant="outline" size="sm"><Plus className="size-3.5" />SQL</Button>
              <Button variant="outline" size="sm"><Plus className="size-3.5" />Python</Button>
              <Button variant="outline" size="sm"><Plus className="size-3.5" />Markdown</Button>
            </div>
          </div>
        </div>
        {showDependencies ? <NotebookDependencyPanel staleCells={staleCells} onClose={() => setShowDependencies(false)} /> : null}
      </div>
    </RedesignPage>
  );
}

export function RedesignDashboardPage({ dashboardId }: { dashboardId: string }) {
  return (
    <RedesignPage>
      <PageHeader
        title={dashboardId}
        subtitle="Dashboard · revenue overview"
        actions={<Button size="sm">Edit</Button>}
      />
      <div className="min-h-0 flex-1 overflow-auto px-3 pb-3">
        <div className="grid gap-3 md:grid-cols-3">
          <MetricCard label="Revenue (30d)" value="$248,300" delta="+12.4%" />
          <MetricCard label="Orders" value="9,328" delta="+4.1%" />
          <MetricCard label="Forecast error" value="3.2%" delta="-0.6%" />
          <SectionCard title="Daily revenue" icon={BarChart3} className="md:col-span-2">
            <div className="h-44"><Sparkline /></div>
          </SectionCard>
          <SectionCard title="Revenue by region" icon={LayoutDashboard}>
            <div className="flex h-44 items-end gap-2">
              {[60, 80, 45, 95, 70].map((height, index) => <div key={index} className="flex-1 rounded-t bg-primary" style={{ height: `${height}%` }} />)}
            </div>
          </SectionCard>
          <RedesignPanel className="md:col-span-3">
            <DelimitedCardHeader>
              <Database className="size-4 text-primary" />
              <DelimitedCardTitle>Top assets</DelimitedCardTitle>
            </DelimitedCardHeader>
            <DelimitedCardContent className="p-0">
              <SimpleTable columns={["Asset", "Rows", "Freshness"]} rows={[["revenue_daily", "30", "fresh"], ["orders_cleaned", "9,328", "fresh"], ["stripe_orders", "14,908", "overdue"]]} />
            </DelimitedCardContent>
          </RedesignPanel>
        </div>
      </div>
    </RedesignPage>
  );
}

function NotebookCell({ type, title, stale, onRun, children }: { type: string; title: string; stale?: boolean; onRun?: () => void; children: ReactNode }) {
  const Icon = kindMeta[type as keyof typeof kindMeta]?.icon ?? BookOpen;
  return (
    <RedesignPanel>
      <DelimitedCardHeader>
        <Icon className="size-4 text-primary" />
        <DelimitedCardTitle>{title}</DelimitedCardTitle>
        <span className="ml-auto rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">{type}</span>
        {stale ? <span className="rounded-full bg-amber-100 px-1.5 py-0.5 text-[10px] text-amber-700"><AlertTriangle className="mr-1 inline size-3" />stale</span> : null}
        {type !== "md" ? <Button variant="ghost" size="icon-sm" onClick={onRun}><Play className="size-3.5" /></Button> : null}
        <Button variant="ghost" size="icon-sm"><MoreHorizontal className="size-3.5" /></Button>
      </DelimitedCardHeader>
      <DelimitedCardContent>{children}</DelimitedCardContent>
    </RedesignPanel>
  );
}

function NotebookDependencyPanel({ staleCells, onClose }: { staleCells: string[]; onClose: () => void }) {
  return (
    <RedesignPanel className="hidden min-h-0 flex-col xl:flex">
      <DelimitedCardHeader>
        <Network className="size-4 text-primary" />
        <DelimitedCardTitle>Dependencies</DelimitedCardTitle>
        <Button variant="ghost" size="icon-sm" className="ml-auto" onClick={onClose}><X className="size-3.5" /></Button>
      </DelimitedCardHeader>
      <DelimitedCardContent className="space-y-2">
        {notebookNodes.map((node) => {
          const Icon = kindMeta[node.kind].icon;
          return (
            <div key={node.id} className="rounded-lg border p-2 text-xs">
              <div className="flex items-center gap-1.5"><Icon className="size-3.5 text-primary" /><span className="min-w-0 flex-1 truncate font-mono">{node.name}</span>{staleCells.includes(node.id) ? <span className="size-1.5 rounded-full bg-amber-500" /> : null}</div>
              <div className="mt-1 text-muted-foreground">{node.description}</div>
            </div>
          );
        })}
        <div className="rounded-lg bg-muted p-2 text-xs text-muted-foreground">Cells form a reactive graph. Editing one marks dependents stale, like a mini pipeline.</div>
        <div className="text-xs text-muted-foreground">Edges: {notebookEdges.map(([from, to]) => `${from} -> ${to}`).join(", ")}</div>
      </DelimitedCardContent>
    </RedesignPanel>
  );
}

function MetricCard({ label, value, delta }: { label: string; value: string; delta: string }) {
  return (
    <SectionCard title={label} icon={LayoutDashboard}>
      <div className="text-2xl font-semibold tracking-tight">{value}</div>
      <div className={`mt-1 text-xs ${delta.startsWith("+") ? "text-emerald-600" : "text-red-500"}`}>{delta}</div>
    </SectionCard>
  );
}

function CodeBlock({ lines }: { lines: string[] }) {
  return <div className="rounded-lg bg-zinc-950 p-3 font-mono text-xs text-zinc-100">{lines.map((line, index) => <div key={index}><span className="mr-3 text-zinc-500">{index + 1}</span>{line}</div>)}</div>;
}

function Sparkline() {
  return (
    <svg viewBox="0 0 100 36" preserveAspectRatio="none" className="h-full w-full">
      <polyline points="0,30 14,22 28,26 42,14 56,18 70,8 84,12 100,4" fill="none" stroke="var(--primary)" strokeWidth="1.5" />
      <polyline points="0,36 0,30 14,22 28,26 42,14 56,18 70,8 84,12 100,4 100,36" fill="var(--primary)" opacity="0.08" stroke="none" />
    </svg>
  );
}
