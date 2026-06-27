import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  CheckCircle2,
  Circle,
  Loader2,
  MoreHorizontal,
  XCircle,
} from "lucide-react";
import { ComponentType, Fragment, ReactNode } from "react";

import { Button } from "@/components/ui/button";
import {
  DelimitedCard,
  DelimitedCardContent,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { AssetStaleness, AssetStalenessStatus } from "@/lib/api-staleness";
import { cn } from "@/lib/utils";

import { RedesignAsset, integrations, kindMeta } from "./redesign-data";

export function RedesignPage({ children }: { children: ReactNode }) {
  return <div className="flex h-full min-h-0 flex-col bg-muted/40 text-foreground">{children}</div>;
}

export function PageHeader({
  title,
  subtitle,
  actions,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="flex min-h-12 shrink-0 items-center gap-3 px-3">
      <div className="min-w-0">
        <h1 className="truncate text-base font-semibold tracking-tight">{title}</h1>
        {subtitle ? <p className="truncate text-xs text-muted-foreground">{subtitle}</p> : null}
      </div>
      <div className="ml-auto flex items-center gap-2">{actions}</div>
    </div>
  );
}

export function RedesignPanel({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return <DelimitedCard className={cn("min-h-0", className)}>{children}</DelimitedCard>;
}

export function SectionCard({
  title,
  icon: Icon,
  children,
  action,
  className,
}: {
  title: string;
  icon?: ComponentType<{ className?: string }>;
  children: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <DelimitedCard className={className}>
      <DelimitedCardHeader>
        {Icon ? <Icon className="size-4 text-primary" /> : null}
        <DelimitedCardTitle>{title}</DelimitedCardTitle>
        <div className="ml-auto">{action}</div>
      </DelimitedCardHeader>
      <DelimitedCardContent>{children}</DelimitedCardContent>
    </DelimitedCard>
  );
}

export function NavLinkButton({
  to,
  icon: Icon,
  label,
}: {
  to: string;
  icon: ComponentType<{ className?: string }>;
  label: string;
}) {
  return (
    <Button asChild size="sm" variant="ghost" className="relative h-12 rounded-none px-3 text-zinc-400 hover:bg-transparent hover:text-zinc-200 data-[state=open]:bg-transparent">
      <Link
        to={to}
        activeOptions={{ exact: to === "/redesign" }}
        activeProps={{ className: "text-white after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-primary" }}
      >
        <Icon className="size-3.5" />
        <span>{label}</span>
      </Link>
    </Button>
  );
}

export function IntegrationBadge({ name }: { name: string }) {
  return (
    <span className="inline-flex max-w-full items-center gap-1.5 rounded-md border bg-background px-1.5 py-0.5 text-[11px] text-muted-foreground">
      <span className="size-2 rounded-sm" style={{ backgroundColor: integrations[name] ?? "#71717a" }} />
      <span className="truncate">{name}</span>
    </span>
  );
}

export function StatusPill({ status }: { status: string }) {
  if (status === "success" || status === "pass" || status === "ok") {
    return <span className="inline-flex items-center gap-1 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[11px] text-emerald-700"><CheckCircle2 className="size-3" />Success</span>;
  }
  if (status === "failed" || status === "fail" || status === "overdue") {
    return <span className="inline-flex items-center gap-1 rounded-full bg-red-100 px-1.5 py-0.5 text-[11px] text-red-700"><XCircle className="size-3" />Failed</span>;
  }
  if (status === "running") {
    return <span className="inline-flex items-center gap-1 rounded-full bg-amber-100 px-1.5 py-0.5 text-[11px] text-amber-700"><Loader2 className="size-3 animate-spin" />Running</span>;
  }
  if (status === "queued") {
    return <span className="inline-flex items-center gap-1 rounded-full bg-sky-100 px-1.5 py-0.5 text-[11px] text-sky-700"><Circle className="size-3" />Queued</span>;
  }
  if (status === "cancelled") {
    return <span className="inline-flex items-center gap-1 rounded-full bg-zinc-200 px-1.5 py-0.5 text-[11px] text-zinc-700"><Circle className="size-3" />Cancelled</span>;
  }
  return <span className="inline-flex items-center gap-1 rounded-full bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground"><Circle className="size-3" />Idle</span>;
}

const stalenessMeta: Record<AssetStalenessStatus, { label: string; className: string; dotClassName: string }> = {
  fresh: { label: "Fresh", className: "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300", dotClassName: "bg-emerald-500" },
  stale_edited: { label: "Edited", className: "bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300", dotClassName: "bg-amber-500" },
  stale_upstream: { label: "Upstream changed", className: "bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-300", dotClassName: "bg-amber-400" },
  partial: { label: "Partial", className: "bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300", dotClassName: "bg-sky-500" },
  never_built: { label: "Never built", className: "bg-zinc-200 text-zinc-700 dark:bg-zinc-500/15 dark:text-zinc-300", dotClassName: "bg-zinc-400" },
  missing: { label: "Missing", className: "bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300", dotClassName: "bg-red-500" },
};

export function stalenessLabel(staleness: AssetStaleness) {
  if (staleness.status === "partial" && staleness.total_seconds && staleness.total_seconds > 0) {
    const day = 24 * 60 * 60;
    if (staleness.total_seconds >= day) {
      return `${Math.floor((staleness.covered_seconds ?? 0) / day)}/${Math.round(staleness.total_seconds / day)} days`;
    }
    const hour = 60 * 60;
    return `${Math.floor((staleness.covered_seconds ?? 0) / hour)}/${Math.round(staleness.total_seconds / hour)} hours`;
  }
  return stalenessMeta[staleness.status]?.label ?? staleness.status;
}

export function StalenessBadge({ staleness, className }: { staleness?: AssetStaleness; className?: string }) {
  if (!staleness) return null;
  const meta = stalenessMeta[staleness.status];
  if (!meta) return null;
  return (
    <span
      data-staleness={staleness.status}
      title={`Staleness: ${stalenessLabel(staleness)}`}
      className={cn("inline-flex items-center gap-1 truncate rounded px-1.5 py-0.5 text-[10px]", meta.className, className)}
    >
      <span className={cn("size-1.5 shrink-0 rounded-full", meta.dotClassName)} />
      {stalenessLabel(staleness)}
    </span>
  );
}

export function stalenessDotClassName(status: AssetStalenessStatus) {
  return stalenessMeta[status]?.dotClassName ?? "bg-zinc-400";
}

export type AssetNodeAction = {
  key: string;
  label: string;
  icon: ComponentType<{ className?: string }>;
  onSelect: () => void;
  destructive?: boolean;
  separatorBefore?: boolean;
};

export function AssetNode({
  asset,
  selected,
  actions,
}: {
  asset: RedesignAsset;
  selected?: boolean;
  actions?: AssetNodeAction[];
}) {
  const meta = kindMeta[asset.kind];
  const Icon = meta.icon;
  const statusMeta = assetNodeStatusMeta(asset.status);
  return (
    <div
      className={cn(
        "w-58 overflow-hidden rounded-xl border-2 bg-card text-left shadow-sm transition hover:border-primary/60",
        selected ? "border-primary" : "border-border"
      )}
    >
      <div className="flex h-8 items-center gap-1.5 border-b bg-muted/30 px-2.5">
        <Icon className="size-3.5 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-mono text-xs font-medium">{asset.name}</span>
        {actions && actions.length > 0 ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              aria-label="Asset actions"
              className="nodrag -mr-1 flex size-5 items-center justify-center rounded text-muted-foreground outline-none hover:bg-muted hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring data-[state=open]:bg-muted data-[state=open]:text-foreground"
              onClick={(event) => event.stopPropagation()}
            >
              <MoreHorizontal className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" onClick={(event) => event.stopPropagation()}>
              <AssetNodeMenuItems actions={actions} ItemComponent={DropdownMenuItem} SeparatorComponent={DropdownMenuSeparator} />
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <MoreHorizontal className="size-3.5 text-muted-foreground" />
        )}
      </div>
      <div className="space-y-2 p-2.5">
        <p className="truncate text-[11px] text-muted-foreground">{asset.description}</p>
        <div className="flex items-center justify-between gap-1.5">
          <span className={cn("truncate rounded px-1.5 py-0.5 text-[10px]", statusMeta.className)}>
            {statusMeta.label} · {asset.materializedAt}
          </span>
          <div className="flex items-center gap-1.5">
            <StalenessBadge staleness={asset.staleness} />
            <IntegrationBadge name={asset.integration} />
          </div>
        </div>
      </div>
    </div>
  );
}

type AssetNodeMenuItemProps = {
  variant?: "default" | "destructive";
  onSelect?: (event: Event) => void;
  className?: string;
  children: ReactNode;
};

export function AssetNodeMenuItems({
  actions,
  ItemComponent,
  SeparatorComponent,
}: {
  actions: AssetNodeAction[];
  ItemComponent: ComponentType<AssetNodeMenuItemProps>;
  SeparatorComponent: ComponentType;
}) {
  return (
    <>
      {actions.map((action) => {
        const ActionIcon = action.icon;
        return (
          <Fragment key={action.key}>
            {action.separatorBefore ? <SeparatorComponent /> : null}
            <ItemComponent
              variant={action.destructive ? "destructive" : "default"}
              onSelect={() => action.onSelect()}
            >
              <ActionIcon className="size-3.5" />
              {action.label}
            </ItemComponent>
          </Fragment>
        );
      })}
    </>
  );
}

function assetNodeStatusMeta(status: RedesignAsset["status"]) {
  if (status === "unknown") {
    return { label: "Unknown", className: "bg-zinc-200 text-zinc-700 dark:bg-zinc-500/15 dark:text-zinc-300" };
  }
  if (status === "pending") {
    return { label: "Running", className: "bg-blue-100 text-blue-700 dark:bg-blue-500/15 dark:text-blue-300" };
  }
  if (status === "failed" || status === "overdue") {
    return { label: status === "overdue" ? "Overdue" : "Failed", className: "bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300" };
  }
  return { label: "Materialized", className: "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300" };
}

export function SimpleTable({
  columns,
  rows,
  className,
  viewportClassName,
}: {
  columns: string[];
  rows: Array<Array<ReactNode>>;
  className?: string;
  // Constrain the scroll viewport (e.g. "max-h-72"): the cap must live on the
  // viewport, where Radix actually scrolls, not on the Root.
  viewportClassName?: string;
}) {
  return (
    <ScrollArea className={cn("h-full min-h-0", className)} viewportClassName={viewportClassName}>
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/50">
            {columns.map((column) => (
              <TableHead key={column} className="h-8 text-xs uppercase text-muted-foreground">{column}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, index) => (
            <TableRow key={index}>
              {row.map((cell, cellIndex) => (
                <TableCell key={cellIndex} className="h-9 py-1.5 text-xs">{cell}</TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </ScrollArea>
  );
}

export function SeverityIcon({ severity }: { severity: string }) {
  if (severity === "error") {
    return <XCircle className="size-4 text-red-500" />;
  }
  if (severity === "warn") {
    return <AlertTriangle className="size-4 text-amber-500" />;
  }
  return <CheckCircle2 className="size-4 text-sky-500" />;
}
