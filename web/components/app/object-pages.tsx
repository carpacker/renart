import { BarChart3, Database, LayoutDashboard } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DelimitedCardContent,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";

import { PageHeader, AppPage, AppPanel, SectionCard, SimpleTable } from "./app-primitives";

export function AppDashboardPage({ dashboardId }: { dashboardId: string }) {
  return (
    <AppPage>
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
          <AppPanel className="md:col-span-3">
            <DelimitedCardHeader>
              <Database className="size-4 text-primary" />
              <DelimitedCardTitle>Top assets</DelimitedCardTitle>
            </DelimitedCardHeader>
            <DelimitedCardContent className="p-0">
              <SimpleTable columns={["Asset", "Rows", "Freshness"]} rows={[["revenue_daily", "30", "fresh"], ["orders_cleaned", "9,328", "fresh"], ["stripe_orders", "14,908", "overdue"]]} />
            </DelimitedCardContent>
          </AppPanel>
        </div>
      </div>
    </AppPage>
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

function Sparkline() {
  return (
    <svg viewBox="0 0 100 36" preserveAspectRatio="none" className="h-full w-full">
      <polyline points="0,30 14,22 28,26 42,14 56,18 70,8 84,12 100,4" fill="none" stroke="var(--primary)" strokeWidth="1.5" />
      <polyline points="0,36 0,30 14,22 28,26 42,14 56,18 70,8 84,12 100,4 100,36" fill="var(--primary)" opacity="0.08" stroke="none" />
    </svg>
  );
}
