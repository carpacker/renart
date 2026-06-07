import { Link, Outlet } from "@tanstack/react-router";
import { Boxes, Cloud, CreditCard, Plug, Plus, Shield, Sliders, User, Users } from "lucide-react";
import { ComponentType } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";

import { IntegrationBadge, PageHeader, RedesignPage, SectionCard, SimpleTable } from "./redesign-primitives";

const projectSections = [
  { id: "general", label: "General", icon: Sliders, to: "/redesign/project/general" },
  { id: "environments", label: "Environments", icon: Boxes, to: "/redesign/project/environments" },
  { id: "connections", label: "Connections", icon: Plug, to: "/redesign/project/connections" },
] as const;

const accountSections = [
  { id: "profile", label: "Account", icon: User, to: "/redesign/account/profile" },
  { id: "members", label: "Members", icon: Users, to: "/redesign/account/members" },
  { id: "workspaces", label: "Workspaces", icon: Cloud, to: "/redesign/account/workspaces" },
  { id: "billing", label: "Billing", icon: CreditCard, to: "/redesign/account/billing" },
] as const;

export function RedesignProjectSettingsShell() {
  return (
    <SettingsShell
      title="Project settings"
      subtitle="data_platform defaults, connections, and environments"
      eyebrow="Project · data_platform"
      sections={projectSections}
    />
  );
}

export function RedesignAccountSettingsShell() {
  return (
    <SettingsShell
      title="Account"
      subtitle="Profile, members, workspaces, and billing"
      eyebrow="Account"
      sections={accountSections}
    />
  );
}

function SettingsShell({
  title,
  subtitle,
  eyebrow,
  sections,
}: {
  title: string;
  subtitle: string;
  eyebrow: string;
  sections: ReadonlyArray<{ id: string; label: string; icon: ComponentType<{ className?: string }>; to: string }>;
}) {
  return (
    <RedesignPage>
      <PageHeader title={title} subtitle={subtitle} />
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 px-3 pb-3 md:grid-cols-[16rem_minmax(0,1fr)]">
        <aside className="hidden min-h-0 md:block">
          <div className="sticky top-0 space-y-2 rounded-xl border bg-zinc-50 p-3">
            <div className="px-2 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{eyebrow}</div>
            {sections.map((section) => (
              <SettingsSideLink key={section.id} section={section} />
            ))}
          </div>
        </aside>
        <div className="min-h-0 overflow-hidden">
          <ScrollArea className="mb-3 md:hidden" horizontalScrollBarClassName="hidden" viewportClassName="w-full">
            <div className="flex gap-2 pb-1">
              {sections.map((section) => (
                <SettingsPillLink key={section.id} section={section} />
              ))}
            </div>
          </ScrollArea>
          <ScrollArea className="h-full min-h-0">
            <div className="mx-auto max-w-4xl">
              <Outlet />
            </div>
          </ScrollArea>
        </div>
      </div>
    </RedesignPage>
  );
}

function SettingsSideLink({ section }: { section: { label: string; icon: ComponentType<{ className?: string }>; to: string } }) {
  return (
    <Link
      to={section.to}
      className="flex h-9 items-center gap-2 rounded-md px-2.5 text-sm text-muted-foreground hover:bg-background hover:text-foreground"
      activeProps={{ className: "bg-background text-foreground shadow-sm font-medium" }}
    >
      <section.icon className="size-4" />
      {section.label}
    </Link>
  );
}

function SettingsPillLink({ section }: { section: { label: string; icon: ComponentType<{ className?: string }>; to: string } }) {
  return (
    <Link to={section.to} className="shrink-0" activeProps={{ className: "text-primary" }}>
      {({ isActive }) => (
        <Badge variant={isActive ? "default" : "outline"} className="h-8 px-3">
          <section.icon className="size-3.5" />
          {section.label}
        </Badge>
      )}
    </Link>
  );
}

export function RedesignProjectGeneralPage() {
  return (
    <SectionCard title="Project" icon={Sliders}>
      <div className="grid gap-4 md:grid-cols-2">
        <Field label="Project name" value="data_platform" />
        <Field label="Default environment" value="default" />
      </div>
    </SectionCard>
  );
}

export function RedesignProjectEnvironmentsPage() {
  return (
    <div className="space-y-4">
      <SectionCard title="Environments" icon={Boxes} action={<Button size="sm"><Plus className="size-3.5" />New</Button>}>
        <div className="space-y-3">
          {["default", "prod"].map((env) => (
            <div key={env} className="rounded-lg border">
              <div className="flex h-11 items-center gap-2 border-b px-3"><Boxes className="size-4 text-muted-foreground" /><span className="font-mono font-medium">{env}</span>{env === "default" ? <span className="rounded bg-amber-100 px-1.5 py-0.5 text-[10px] text-amber-700">DEFAULT</span> : null}<Button className="ml-auto" variant="outline" size="sm">Edit</Button></div>
              <div className="flex h-10 items-center justify-between px-3 text-sm"><span className="font-mono">{env === "prod" ? "bq-prod" : "duckdb-default"}</span><IntegrationBadge name={env === "prod" ? "BigQuery" : "DuckDB"} /></div>
            </div>
          ))}
        </div>
      </SectionCard>
    </div>
  );
}

export function RedesignProjectConnectionsPage() {
  return (
    <SectionCard title="Connections" icon={Plug} action={<Button size="sm"><Plus className="size-3.5" />Add</Button>}>
      <SimpleTable columns={["Connection", "Type", "Environment", "Action"]} rows={[[<span className="font-mono" key="n">duckdb-default</span>, <IntegrationBadge key="i" name="DuckDB" />, "default", <Button key="b" variant="outline" size="xs">View</Button>], [<span className="font-mono" key="n">bq-prod</span>, <IntegrationBadge key="i" name="BigQuery" />, "prod", <Button key="b" variant="outline" size="xs">View</Button>], [<span className="font-mono" key="n">stripe</span>, <IntegrationBadge key="i" name="Stripe" />, "prod", <Button key="b" variant="outline" size="xs">View</Button>]]} />
    </SectionCard>
  );
}

export function RedesignAccountProfilePage() {
  return (
    <SectionCard title="Profile" icon={User}>
      <div className="grid gap-4 md:grid-cols-2">
        <Field label="Name" value="Jane Doe" />
        <Field label="Email" value="jane@acme.io" />
      </div>
    </SectionCard>
  );
}

export function RedesignAccountMembersPage() {
  return (
    <SectionCard title="Members & permissions" icon={Users} action={<Button size="sm"><Plus className="size-3.5" />Invite</Button>}>
      <SimpleTable columns={["Member", "Email", "Role"]} rows={[["Jane Doe", "jane@acme.io", "Owner"], ["Lukas R.", "lukas@acme.io", "Editor"], ["Sam Lee", "sam@acme.io", "Viewer"], ["CI Bot", "ci@acme.io", "Editor"]]} />
      <p className="mt-3 flex items-center gap-2 text-xs text-muted-foreground"><Shield className="size-3.5" />Owner manages billing and members, Editor builds pipelines, Viewer is read-only.</p>
    </SectionCard>
  );
}

export function RedesignAccountWorkspacesPage() {
  return (
    <div className="space-y-4">
      {["data_platform", "marketing_analytics"].map((workspace, index) => (
        <SectionCard key={workspace} title={workspace} icon={index === 0 ? Cloud : Boxes} action={index === 0 ? <span className="rounded-full bg-emerald-100 px-2 py-0.5 text-xs text-emerald-700">Connected</span> : <Button size="sm">Connect</Button>}>
          <p className="text-sm text-muted-foreground">{index === 0 ? "Cloud workspace · branch main · synced 2m ago" : "Local workspace · not connected"}</p>
        </SectionCard>
      ))}
    </div>
  );
}

export function RedesignAccountBillingPage() {
  return (
    <SectionCard title="Billing" icon={CreditCard} action={<Button variant="outline" size="sm">Manage plan</Button>}>
      <div className="grid gap-3 md:grid-cols-3">
        {[["Seats used", "4 / 5"], ["Pipeline runs", "1,284 / mo"], ["Cloud compute", "38 hrs"]].map(([label, value]) => <div key={label} className="rounded-lg border bg-muted/30 p-3"><div className="text-xs text-muted-foreground">{label}</div><div className="mt-1 text-lg font-semibold">{value}</div></div>)}
      </div>
    </SectionCard>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return <div className="space-y-1.5"><Label>{label}</Label><Input defaultValue={value} /></div>;
}
