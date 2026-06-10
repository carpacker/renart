import { Link, Outlet } from "@tanstack/react-router";
import { Bell, Bot, Building2, Check, ChevronDown, Cloud, CreditCard, FileCode, GitBranch, GitCommit, Loader2, LogOut, Plus, RefreshCw, Search, Send, Settings, Sparkles, User, Users } from "lucide-react";
import { ReactNode, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useSchedulerEvents } from "@/hooks/use-scheduler-events";
import { useSourceControl } from "@/hooks/use-source-control";
import type { SourceControlChange } from "@/lib/types";

import { navItems } from "./redesign-data";
import { NavLinkButton } from "./redesign-primitives";

export function RedesignShell() {
  useSchedulerEvents();
  const sourceControl = useSourceControl();

  return (
    <div className="flex h-screen min-h-0 flex-col bg-zinc-100 text-zinc-950">
      <header className="flex h-12 shrink-0 items-center border-b border-zinc-800 bg-zinc-950 px-2 text-zinc-100 sm:px-3">
        <Link to="/redesign" className="flex items-center gap-2 pr-2 sm:pr-3">
          <div className="flex size-7 items-center justify-center rounded-lg bg-primary text-sm font-bold text-primary-foreground">R</div>
          <span className="hidden font-semibold tracking-tight sm:inline">Renart</span>
        </Link>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" className="h-7 border-zinc-800 bg-zinc-950 px-2 text-zinc-200 hover:bg-zinc-800 hover:text-white">
              <Building2 className="size-3.5 text-zinc-400" />
              <span className="max-w-32 truncate font-medium sm:max-w-44">data_platform</span>
              <Cloud className="size-3 text-primary" />
              <ChevronDown className="size-3 text-zinc-500" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-64">
            <DropdownMenuLabel>Projects</DropdownMenuLabel>
            <DropdownMenuItem>
              <Building2 className="size-4" />
              <span className="flex-1">data_platform</span>
              <Cloud className="size-3.5 text-primary" />
              <Check className="size-3.5" />
            </DropdownMenuItem>
            <DropdownMenuItem>
              <Building2 className="size-4" />
              <span className="flex-1">marketing_analytics</span>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link to="/redesign/project/general"><Settings className="size-4" />Project settings</Link>
            </DropdownMenuItem>
            <DropdownMenuItem>
              <Plus className="size-4" />New project
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link to="/redesign/account/workspaces"><Cloud className="size-4" />Connect cloud workspace</Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <Separator orientation="vertical" className="mx-2 hidden h-5 bg-zinc-800 md:block" />

        <nav className="hidden items-center md:flex">
          {navItems.map((item) => (
            <NavLinkButton key={item.to} {...item} />
          ))}
        </nav>

        <div className="flex-1" />

        <Sheet>
          <SheetTrigger asChild>
            <Button aria-label="Source control" variant="ghost" size="sm" className="h-8 px-2 text-zinc-300 hover:bg-zinc-800 hover:text-white">
              <GitBranch className="size-4" />
              <span className="hidden max-w-32 truncate sm:inline">{sourceControl.repository?.branch || "git"}</span>
              {sourceControl.repository && sourceControl.repository.changes.length > 0 ? <span className="rounded bg-zinc-800 px-1 text-[10px] text-zinc-400">{sourceControl.repository.changes.length}</span> : null}
            </Button>
          </SheetTrigger>
          <GitSheet sourceControl={sourceControl} />
        </Sheet>
        <Button variant="ghost" size="icon" className="size-8 text-zinc-400 hover:bg-zinc-800 hover:text-white">
          <Search className="size-4" />
        </Button>
        <Button variant="ghost" size="icon" className="size-8 text-zinc-400 hover:bg-zinc-800 hover:text-white">
          <Bell className="size-4" />
        </Button>
        <Sheet>
          <SheetTrigger asChild>
            <Button variant="ghost" size="icon" className="size-8 text-zinc-400 hover:bg-zinc-800 hover:text-white">
              <Sparkles className="size-4" />
            </Button>
          </SheetTrigger>
          <AiSheet />
        </Sheet>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="size-8 rounded-full bg-teal-600 text-white hover:bg-teal-700">
              <User className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-60">
            <div className="px-2 py-2">
              <div className="text-sm font-medium">Jane Doe</div>
              <div className="text-xs text-muted-foreground">jane@acme.io · Owner</div>
            </div>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link to="/redesign/account/profile"><User className="size-4" />Account settings</Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link to="/redesign/account/members"><Users className="size-4" />Members & permissions</Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link to="/redesign/account/billing"><CreditCard className="size-4" />Billing</Link>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem>
              <LogOut className="size-4" />Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </header>

      <main className="min-h-0 flex-1 overflow-hidden">
        <Outlet />
      </main>

      <nav className="grid h-14 shrink-0 grid-cols-4 border-t bg-white md:hidden">
        {navItems.map((item) => (
          <Link key={item.to} to={item.to} activeOptions={{ exact: item.to === "/redesign" }} activeProps={{ className: "text-primary" }} className="flex flex-col items-center justify-center gap-0.5 text-[10px] text-muted-foreground">
            <item.icon className="size-4" />
            {item.label}
          </Link>
        ))}
      </nav>
    </div>
  );
}

function AiSheet() {
  return (
    <SheetContent className="w-full sm:max-w-md">
      <SheetHeader>
        <SheetTitle className="flex items-center gap-2"><Sparkles className="size-4 text-primary" />AI builder</SheetTitle>
        <SheetDescription>Preview AI-assisted pipeline changes before applying them.</SheetDescription>
      </SheetHeader>
      <div className="flex-1 space-y-3 overflow-auto px-4">
        <ChatBubble who="user">Add a Stripe source and a daily revenue rollup.</ChatBubble>
        <ChatBubble who="ai">I'll create <span className="font-mono">stripe_orders</span>, <span className="font-mono">revenue_daily</span>, and wire the lineage edge.</ChatBubble>
        <div className="flex gap-2 rounded-lg border bg-muted/40 p-3 text-xs">
          <Button size="xs">Apply</Button>
          <Button variant="outline" size="xs">Preview diff</Button>
        </div>
      </div>
      <SheetFooter>
        <div className="flex items-end gap-2 rounded-xl border bg-background p-2">
          <textarea className="min-h-8 flex-1 resize-none bg-transparent px-1 text-sm outline-none" placeholder="Describe a change..." />
          <Button size="icon-sm"><Send className="size-3.5" /></Button>
        </div>
      </SheetFooter>
    </SheetContent>
  );
}

function GitSheet({ sourceControl }: { sourceControl: ReturnType<typeof useSourceControl> }) {
  const { repository, branches, loading, busy, diffLoading, diff, error, refresh, loadDiff, stage, unstage, checkout, commit } = sourceControl;
  const [message, setMessage] = useState("");
  const changes = repository?.changes ?? [];
  const stagedChanges = changes.filter((change) => change.staged);
  const unstagedChanges = changes.filter((change) => !change.staged);
  const changedCount = changes.length;
  const branch = repository?.branch || "unknown";

  const submitCommit = async () => {
    await commit(message);
    setMessage("");
  };

  return (
    <SheetContent className="w-full sm:max-w-md">
      <SheetHeader>
        <SheetTitle className="flex items-center gap-2"><GitBranch className="size-4 text-primary" />Source control</SheetTitle>
        <SheetDescription>Review, stage, and commit local workspace changes.</SheetDescription>
      </SheetHeader>
      <div className="flex-1 space-y-4 overflow-auto px-4 text-sm">
        <div className="flex items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" className="min-w-0 flex-1 justify-start" disabled={busy || loading}>
                <GitBranch className="size-4" />
                <span className="truncate">{branch}</span>
                <ChevronDown className="ml-auto size-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-64">
              <DropdownMenuLabel>Local branches</DropdownMenuLabel>
              {branches.map((item) => (
                <DropdownMenuItem key={item} disabled={busy || item === branch} onClick={() => void checkout(item)}>
                  <GitBranch className="size-4" />
                  <span className="flex-1 truncate">{item}</span>
                  {item === branch ? <Check className="size-3.5" /> : null}
                </DropdownMenuItem>
              ))}
              {branches.length === 0 ? <DropdownMenuItem disabled>No branches found</DropdownMenuItem> : null}
            </DropdownMenuContent>
          </DropdownMenu>
          <Button variant="ghost" size="icon-sm" disabled={busy || loading} onClick={() => void refresh()}>
            {loading ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
          </Button>
        </div>
        {error ? <div className="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div> : null}
        <div className="space-y-2">
          <textarea className="min-h-16 w-full resize-none rounded-lg border bg-background p-2 text-sm outline-none" placeholder="Commit message..." value={message} onChange={(event) => setMessage(event.target.value)} />
          <Button className="w-full" disabled={busy || stagedChanges.length === 0 || !message.trim()} onClick={() => void submitCommit()}>
            {busy ? <Loader2 className="size-4 animate-spin" /> : <GitCommit className="size-4" />}Commit {stagedChanges.length} file{stagedChanges.length === 1 ? "" : "s"}
          </Button>
        </div>
        <ChangeGroup title={`Staged · ${stagedChanges.length}`} changes={stagedChanges} actionLabel="Unstage" busy={busy} selectedPath={diff?.staged ? diff.path : ""} onSelect={(path) => void loadDiff(path, true)} onAction={(path) => void unstage([path])} />
        <ChangeGroup title={`Changes · ${unstagedChanges.length}`} changes={unstagedChanges} actionLabel="Stage" busy={busy} selectedPath={!diff?.staged ? diff?.path : ""} onSelect={(path) => void loadDiff(path, false)} onAction={(path) => void stage([path])} />
        {changedCount > 0 ? (
          <div className="grid grid-cols-2 gap-2">
            <Button variant="outline" size="sm" disabled={busy || unstagedChanges.length === 0} onClick={() => void stage(unstagedChanges.map((change) => change.path))}>Stage all</Button>
            <Button variant="outline" size="sm" disabled={busy || stagedChanges.length === 0} onClick={() => void unstage(stagedChanges.map((change) => change.path))}>Unstage all</Button>
          </div>
        ) : null}
        {!loading && changedCount === 0 ? <div className="rounded-lg border border-dashed p-6 text-center text-xs text-muted-foreground">Working tree clean.</div> : null}
        <DiffViewer diff={diff} loading={diffLoading} />
      </div>
    </SheetContent>
  );
}

function ChangeGroup({ title, changes, actionLabel, busy, selectedPath, onSelect, onAction }: { title: string; changes: SourceControlChange[]; actionLabel: string; busy: boolean; selectedPath?: string; onSelect: (path: string) => void; onAction: (path: string) => void }) {
  if (changes.length === 0) {
    return null;
  }
  return (
    <div>
      <div className="mb-2 text-xs font-semibold uppercase text-muted-foreground">{title}</div>
      {changes.map((change) => (
        <div key={`${title}-${change.path}`} className={`group flex min-h-8 items-center gap-2 rounded-md px-2 hover:bg-muted ${selectedPath === change.path ? "bg-muted" : ""}`}>
          <FileCode className="size-3.5 shrink-0 text-primary" />
          <button className="min-w-0 flex-1 truncate text-left font-mono text-xs" onClick={() => onSelect(change.path)}>{change.path}</button>
          <span className="font-mono text-xs text-amber-600">{sourceControlStatusLabel(change)}</span>
          <Button variant="ghost" size="xs" className="opacity-0 group-hover:opacity-100" disabled={busy} onClick={() => onAction(change.path)}>{actionLabel}</Button>
        </div>
      ))}
    </div>
  );
}

function DiffViewer({ diff, loading }: { diff: { path: string; staged: boolean; patch: string } | null; loading: boolean }) {
  if (loading) {
    return <div className="flex items-center gap-2 rounded-lg border p-3 text-xs text-muted-foreground"><Loader2 className="size-3.5 animate-spin" />Loading diff...</div>;
  }
  if (!diff) {
    return <div className="rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">Select a changed file to preview its diff.</div>;
  }
  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="flex h-8 items-center gap-2 border-b bg-muted/50 px-2 text-xs">
        <span className="min-w-0 flex-1 truncate font-mono">{diff.path}</span>
        <span className="rounded bg-background px-1.5 py-0.5 text-[10px] uppercase text-muted-foreground">{diff.staged ? "staged" : "worktree"}</span>
      </div>
      <ScrollArea className="max-h-72 bg-zinc-950" viewportClassName="max-h-72">
        <pre className="whitespace-pre p-3 font-mono text-[11px] leading-relaxed text-zinc-100">{diff.patch || "No textual diff available."}</pre>
      </ScrollArea>
    </div>
  );
}

function sourceControlStatusLabel(change: SourceControlChange) {
  const staged = change.staged_status.trim();
  const worktree = change.worktree_status.trim();
  return staged || worktree || "M";
}

function ChatBubble({ who, children }: { who: "user" | "ai"; children: ReactNode }) {
  const ai = who === "ai";
  return (
    <div className={`flex gap-2 ${ai ? "" : "flex-row-reverse"}`}>
      <div className={`flex size-7 shrink-0 items-center justify-center rounded-full ${ai ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"}`}>
        {ai ? <Bot className="size-3.5" /> : "J"}
      </div>
      <div className={`max-w-72 rounded-xl px-3 py-2 text-xs ${ai ? "bg-muted" : "bg-primary text-primary-foreground"}`}>{children}</div>
    </div>
  );
}
