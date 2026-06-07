import { Link, Outlet } from "@tanstack/react-router";
import { Bell, Bot, Building2, Check, ChevronDown, Cloud, CreditCard, FileCode, GitBranch, GitCommit, GitPullRequest, LogOut, Plus, Search, Send, Settings, Sparkles, User, Users } from "lucide-react";
import { ReactNode } from "react";

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
import { useSchedulerEvents } from "@/hooks/use-scheduler-events";

import { navItems } from "./redesign-data";
import { NavLinkButton } from "./redesign-primitives";

export function RedesignShell() {
  useSchedulerEvents();

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
            <Button variant="ghost" size="sm" className="h-8 px-2 text-zinc-300 hover:bg-zinc-800 hover:text-white">
              <GitBranch className="size-4" />
              <span className="hidden sm:inline">main</span>
              <span className="rounded bg-zinc-800 px-1 text-[10px] text-zinc-400">3</span>
            </Button>
          </SheetTrigger>
          <GitSheet />
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

function GitSheet() {
  return (
    <SheetContent className="w-full sm:max-w-md">
      <SheetHeader>
        <SheetTitle className="flex items-center gap-2"><GitBranch className="size-4 text-primary" />Source control</SheetTitle>
        <SheetDescription>Static Git status mock for the redesign shell.</SheetDescription>
      </SheetHeader>
      <div className="flex-1 space-y-4 overflow-auto px-4 text-sm">
        <Button variant="outline" className="w-full justify-start"><GitBranch className="size-4" />main<ChevronDown className="ml-auto size-3.5" /></Button>
        <div className="space-y-2">
          <textarea className="min-h-16 w-full resize-none rounded-lg border bg-background p-2 text-sm outline-none" placeholder="Commit message..." />
          <Button className="w-full"><GitCommit className="size-4" />Commit 3 files</Button>
        </div>
        <div>
          <div className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Changes · 3</div>
          {[['revenue_daily.sql', 'M'], ['stripe_orders.yml', 'A'], ['pipeline.yml', 'M']].map(([file, status]) => (
            <div key={file} className="flex h-8 items-center gap-2 rounded-md px-2 hover:bg-muted">
              <FileCode className="size-3.5 text-primary" />
              <span className="min-w-0 flex-1 truncate font-mono text-xs">{file}</span>
              <span className="font-mono text-xs text-amber-600">{status}</span>
            </div>
          ))}
        </div>
        <div>
          <div className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Pull requests</div>
          {[['12', 'Add Stripe source', 'open'], ['9', 'Switch small to view', 'merged']].map(([id, title, state]) => (
            <div key={id} className="flex items-start gap-2 rounded-md px-2 py-2 hover:bg-muted">
              <GitPullRequest className="mt-0.5 size-4 text-primary" />
              <div className="min-w-0"><div className="truncate text-xs">#{id} {title}</div><div className="text-[11px] text-muted-foreground">{state}</div></div>
            </div>
          ))}
        </div>
      </div>
    </SheetContent>
  );
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
