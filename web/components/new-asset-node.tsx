"use client";

import { Database, Hammer, Workflow } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { NodeProps } from "reactflow";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

export type NewAssetKind = "sql" | "python" | "ingestr";

export type NewAssetNodeData = {
  name: string;
  kind: NewAssetKind;
  onKindChange: (kind: NewAssetKind) => string;
  onCreate: (name: string) => void;
  onCancel: () => void;
};

export function NewAssetNode({ data }: NodeProps<NewAssetNodeData>) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [name, setName] = useState(data.name);

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });

    return () => window.cancelAnimationFrame(frame);
  }, []);

  return (
    <div
      className="min-w-72 rounded-lg border-2 border-primary/40 bg-card p-2 shadow-sm"
      data-new-asset-node="true"
    >
      <Tabs
        onValueChange={(value) =>
          setName(data.onKindChange(value as NewAssetKind))
        }
        value={data.kind}
      >
        <TabsList className="nodrag mb-2 grid w-full grid-cols-3">
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
        </TabsList>
      </Tabs>

      <div className="nodrag flex items-center gap-2">
        <Input
          ref={inputRef}
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
          Create
        </Button>
      </div>
    </div>
  );
}
