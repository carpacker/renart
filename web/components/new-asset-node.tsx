"use client";

import { Boxes, Database, Globe, Hammer, Workflow } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Handle, NodeProps, Position } from "reactflow";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

export type NewAssetKind = "sql" | "python" | "ingestr" | "sling" | "api";

export type NewAssetNodeData = {
  name: string;
  kind: NewAssetKind;
  onKindChange: (kind: NewAssetKind) => string;
  onCreate: (name: string) => void;
  onCancel: () => void;
  createLabel?: string;
  kindLocked?: boolean;
};

export function NewAssetNode({ data }: NodeProps<NewAssetNodeData>) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [name, setName] = useState(data.name);

  useEffect(() => {
    setName(data.name);
  }, [data.name]);

  useLayoutEffect(() => {
    focusNameInput(inputRef.current);
    const frame = window.requestAnimationFrame(() => {
      focusNameInput(inputRef.current);
    });
    const timeout = window.setTimeout(() => {
      focusNameInput(inputRef.current);
    }, 50);
    const interval = window.setInterval(() => {
      focusNameInput(inputRef.current);
    }, 75);
    const intervalTimeout = window.setTimeout(() => {
      window.clearInterval(interval);
    }, 900);

    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timeout);
      window.clearInterval(interval);
      window.clearTimeout(intervalTimeout);
    };
  }, [data.name]);

  return (
    <div
      className="min-w-72 rounded-lg border-2 border-primary/40 bg-card p-2 shadow-sm"
      data-new-asset-node="true"
    >
      <Handle className="asset-node-hidden-handle" type="target" position={Position.Top} />
      {data.kindLocked ? (
        <div className="nodrag mb-2 px-1 text-sm font-medium">
          New child asset
        </div>
      ) : (
        <Tabs
          onValueChange={(value) =>
            setName(data.onKindChange(value as NewAssetKind))
          }
          value={data.kind}
        >
          <TabsList className="nodrag mb-2 grid w-full grid-cols-5">
            <TabsTrigger className="nodrag" value="sql">
              <Database className="mr-1 size-3.5 text-emerald-600" />
              SQL
            </TabsTrigger>
            <TabsTrigger className="nodrag" value="python">
              <Hammer className="mr-1 size-3.5 text-amber-600" />
              Python
            </TabsTrigger>
            <TabsTrigger className="nodrag" value="ingestr">
              <Workflow className="mr-1 size-3.5 text-sky-600" />
              Ingestr
            </TabsTrigger>
            <TabsTrigger className="nodrag" value="sling">
              <Boxes className="mr-1 size-3.5 text-violet-600" />
              Sling
            </TabsTrigger>
            <TabsTrigger className="nodrag" value="api">
              <Globe className="mr-1 size-3.5 text-cyan-600" />
              API
            </TabsTrigger>
          </TabsList>
        </Tabs>
      )}

      <div className="nodrag flex items-center gap-2">
        <Input
          ref={inputRef}
          autoFocus
          className="h-8 text-base md:text-sm"
          onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              data.onCreate(name);
            }
            if (event.key === "Escape") {
              event.preventDefault();
              data.onCancel();
            }
          }}
          placeholder="Asset name"
          value={name}
        />
        <Button
          className="nodrag"
          disabled={!name.trim()}
          onClick={() => data.onCreate(name)}
          size="sm"
          type="button"
        >
          {data.createLabel ?? "Create"}
        </Button>
      </div>
    </div>
  );
}

function focusNameInput(input: HTMLInputElement | null) {
  if (!input) {
    return;
  }

  if (document.activeElement === input) {
    return;
  }

  input.focus({ preventScroll: true });
  input.select();
}
