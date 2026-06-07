import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  CheckCircle2,
  Circle,
  Loader2,
  MoreHorizontal,
  XCircle,
} from "lucide-react";
import { ComponentType, ReactNode } from "react";

import { Button } from "@/components/ui/button";
import {
  DelimitedCard,
  DelimitedCardContent,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

import { RedesignAsset, integrations, kindMeta } from "./redesign-data";

export function RedesignPage({ children }: { children: ReactNode }) {
  return <div className="flex h-full min-h-0 flex-col bg-zinc-100 text-zinc-950">{children}</div>;
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

export function AssetNode({ asset, selected }: { asset: RedesignAsset; selected?: boolean }) {
  const meta = kindMeta[asset.kind];
  const Icon = meta.icon;
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
        <MoreHorizontal className="size-3.5 text-muted-foreground" />
      </div>
      <div className="space-y-2 p-2.5">
        <p className="truncate text-[11px] text-muted-foreground">{asset.description}</p>
        <div className="flex items-center justify-between gap-1.5">
          <span className={cn("truncate rounded px-1.5 py-0.5 text-[10px]", asset.status === "overdue" ? "bg-red-100 text-red-700" : "bg-emerald-100 text-emerald-700")}>
            {asset.status === "overdue" ? "Overdue" : "Materialized"} · {asset.materializedAt}
          </span>
          <IntegrationBadge name={asset.integration} />
        </div>
      </div>
    </div>
  );
}

export function SimpleTable({
  columns,
  rows,
}: {
  columns: string[];
  rows: Array<Array<ReactNode>>;
}) {
  return (
    <ScrollArea className="h-full min-h-0">
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
