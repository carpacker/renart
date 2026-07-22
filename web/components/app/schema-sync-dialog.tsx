"use client";

import { useEffect, useMemo, useState } from "react";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type {
  ColumnSchemaMergeRow,
  ColumnSchemaResolution,
  ColumnSchemaSourceSnapshot,
  ColumnSchemaSyncResult,
  WebColumn,
} from "@/lib/generated/api-types";
import { cn } from "@/lib/utils";

const CURRENT_CHOICE = "current";
const REMOVE_CHOICE = "remove";
const sourceChoice = (sourceID: string) => `source:${sourceID}`;

type SchemaSyncDialogProps = {
  open: boolean;
  result: ColumnSchemaSyncResult | null;
  applying: boolean;
  error?: string | null;
  onOpenChange: (open: boolean) => void;
  onApply: (resolutions: ColumnSchemaResolution[]) => void;
};

export function SchemaSyncDialog({
  open,
  result,
  applying,
  error,
  onOpenChange,
  onApply,
}: SchemaSyncDialogProps) {
  const [choices, setChoices] = useState<Record<string, string>>({});
  const conflictRows = useMemo(() => result?.rows.filter((row) => row.conflict) ?? [], [result]);
  const notes = useMemo(
    () => [
      ...(result?.notes ?? []),
      ...(result?.sources.flatMap((source) => source.notes ?? []) ?? []),
    ],
    [result],
  );

  useEffect(() => {
    if (!result) {
      setChoices({});
      return;
    }
    const next: Record<string, string> = {};
    for (const row of result.rows) {
      if (row.conflict) {
        next[columnKey(row.column)] = defaultChoice(row, result.sources);
      }
    }
    setChoices(next);
  }, [result]);

  const apply = () => {
    if (!result) return;
    const resolutions = conflictRows.map((row) =>
      resolutionForChoice(row, choices[columnKey(row.column)], result.sources),
    );
    onApply(resolutions);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90dvh] min-w-0 flex-col overflow-hidden sm:max-w-[min(96vw,80rem)]">
        <DialogHeader className="shrink-0 pr-8">
          <DialogTitle>Resolve schema differences</DialogTitle>
          <DialogDescription>
            Compare the asset inference, selected advisory sources, and saved metadata. Choose the
            schema Renart should keep for each highlighted difference.
          </DialogDescription>
        </DialogHeader>

        {notes.length > 0 ? (
          <Alert className="shrink-0">
            <AlertDescription>
              {notes.map((note, index) => (
                <p key={`${index}:${note}`}>{note}</p>
              ))}
            </AlertDescription>
          </Alert>
        ) : null}

        {result ? (
          <ScrollArea
            className="min-h-0 flex-1 rounded-md border"
            viewportClassName="max-h-[65dvh]"
          >
            <Table className="min-w-[900px]">
              <TableHeader>
                <TableRow>
                  <TableHead className="min-w-64">Column</TableHead>
                  {result.sources.map((snapshot) => (
                    <TableHead key={snapshot.source.id} className="min-w-44">
                      <div className="flex flex-col gap-0.5">
                        <span>{snapshot.source.label}</span>
                        <span className="font-normal text-muted-foreground">
                          {snapshot.source.category === "definition"
                            ? "Asset inference"
                            : snapshot.fresh === true
                              ? "Fresh output"
                              : snapshot.fresh === false
                                ? "Stale output"
                                : "Advisory"}
                          {snapshot.sample_records !== undefined
                            ? ` · ${snapshot.sample_records} sampled`
                            : ""}
                        </span>
                      </div>
                    </TableHead>
                  ))}
                  <TableHead className="min-w-44">Saved metadata</TableHead>
                  <TableHead className="min-w-60">Result</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {result.rows.map((row) => (
                  <TableRow
                    key={columnKey(row.column)}
                    className={cn(
                      row.conflict && "bg-destructive/5",
                      !row.conflict &&
                        (row.kind === "added" || row.kind === "type_filled") &&
                        "bg-primary/5",
                    )}
                  >
                    <TableCell className="min-w-64 whitespace-normal">
                      <div className="flex flex-col gap-1">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-mono font-medium">{row.column}</span>
                          <SchemaRowBadge row={row} />
                        </div>
                        <span className="text-muted-foreground">{row.detail}</span>
                      </div>
                    </TableCell>
                    {result.sources.map((snapshot) => (
                      <TableCell key={snapshot.source.id}>
                        <SourceSchemaValue row={row} snapshot={snapshot} />
                      </TableCell>
                    ))}
                    <TableCell>
                      <SchemaValue
                        present={row.current_present}
                        column={row.column}
                        type={row.current_type}
                      />
                    </TableCell>
                    <TableCell>
                      {row.conflict ? (
                        <ResolutionSelect
                          row={row}
                          sources={result.sources}
                          value={choices[columnKey(row.column)] ?? REMOVE_CHOICE}
                          onValueChange={(value) =>
                            setChoices((current) => ({
                              ...current,
                              [columnKey(row.column)]: value,
                            }))
                          }
                        />
                      ) : (
                        <SchemaValue
                          present={row.proposed_present}
                          column={row.column}
                          type={row.proposed_type}
                        />
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </ScrollArea>
        ) : null}

        {error ? (
          <p role="alert" className="shrink-0 text-destructive">
            {error}
          </p>
        ) : null}

        <DialogFooter className="shrink-0">
          <Button variant="outline" disabled={applying} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={applying || !result || conflictRows.length === 0} onClick={apply}>
            {applying ? <Spinner data-icon="inline-start" /> : null}
            Apply resolution
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ResolutionSelect({
  row,
  sources,
  value,
  onValueChange,
}: {
  row: ColumnSchemaMergeRow;
  sources: ColumnSchemaSourceSnapshot[];
  value: string;
  onValueChange: (value: string) => void;
}) {
  const options = resolutionOptions(row, sources);
  const selected = options.find((option) => option.value === value) ?? options[0];
  return (
    <Select value={value} onValueChange={onValueChange}>
      <SelectTrigger size="sm" className="w-full">
        <SelectValue>{selected?.label ?? "Choose a result"}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function resolutionOptions(row: ColumnSchemaMergeRow, sources: ColumnSchemaSourceSnapshot[]) {
  const options: Array<{ value: string; label: string }> = [];
  if (row.current_present) {
    options.push({
      value: CURRENT_CHOICE,
      label: `Keep saved · ${schemaValueLabel(row.column, row.current_type)}`,
    });
  }
  for (const snapshot of sources) {
    const column = sourceColumn(snapshot, row.column);
    if (!column) continue;
    options.push({
      value: sourceChoice(snapshot.source.id),
      label: `Use ${snapshot.source.label} · ${schemaValueLabel(row.column, column.type)}`,
    });
  }
  options.push({ value: REMOVE_CHOICE, label: "Remove column" });
  return options;
}

function defaultChoice(row: ColumnSchemaMergeRow, sources: ColumnSchemaSourceSnapshot[]) {
  const primary =
    sources.find((snapshot) => snapshot.source.category === "definition") ?? sources[0];
  if (row.kind === "source_missing" && primary && sourceColumn(primary, row.column)) {
    return sourceChoice(primary.source.id);
  }
  if (row.current_present) {
    return CURRENT_CHOICE;
  }
  if (row.proposed_present && primary && sourceColumn(primary, row.column)) {
    return sourceChoice(primary.source.id);
  }
  return REMOVE_CHOICE;
}

function resolutionForChoice(
  row: ColumnSchemaMergeRow,
  choice: string | undefined,
  sources: ColumnSchemaSourceSnapshot[],
): ColumnSchemaResolution {
  if (choice === CURRENT_CHOICE) {
    return { column: row.column, action: "use", source: "current", type: row.current_type };
  }
  if (choice?.startsWith("source:")) {
    const sourceID = choice.slice("source:".length);
    const snapshot = sources.find((source) => source.source.id === sourceID);
    const column = snapshot ? sourceColumn(snapshot, row.column) : undefined;
    return {
      column: row.column,
      action: "use",
      source: sourceID,
      type: column?.type ?? "",
    };
  }
  return { column: row.column, action: "remove" };
}

function SourceSchemaValue({
  row,
  snapshot,
}: {
  row: ColumnSchemaMergeRow;
  snapshot: ColumnSchemaSourceSnapshot;
}) {
  const column = sourceColumn(snapshot, row.column);
  return (
    <SchemaValue
      present={Boolean(column)}
      column={row.column}
      type={column ? column.type : row.current_present ? row.current_type : row.proposed_type}
    />
  );
}

function SchemaValue({
  present,
  column,
  type,
}: {
  present: boolean;
  column: string;
  type?: string;
}) {
  if (!present) {
    return (
      <span className="font-mono text-muted-foreground">
        <span className="line-through">{column}</span>: missing
      </span>
    );
  }
  return <span className="font-mono">{schemaValueLabel(column, type)}</span>;
}

function SchemaRowBadge({ row }: { row: ColumnSchemaMergeRow }) {
  if (row.conflict) {
    return (
      <Badge variant="destructive" size="xs">
        Review
      </Badge>
    );
  }
  if (row.kind === "added") {
    return (
      <Badge variant="secondary" size="xs">
        Add
      </Badge>
    );
  }
  if (row.kind === "type_filled") {
    return (
      <Badge variant="secondary" size="xs">
        Fill type
      </Badge>
    );
  }
  if (row.kind === "owned") {
    return (
      <Badge variant="outline" size="xs">
        Owned
      </Badge>
    );
  }
  if (row.kind === "provenance") {
    return (
      <Badge variant="outline" size="xs">
        Sourced
      </Badge>
    );
  }
  if (row.kind === "manual") {
    return (
      <Badge variant="muted" size="xs">
        Manual
      </Badge>
    );
  }
  return null;
}

function sourceColumn(snapshot: ColumnSchemaSourceSnapshot, name: string): WebColumn | undefined {
  const key = columnKey(name);
  return snapshot.columns.find((column) => columnKey(column.name) === key);
}

function columnKey(name: string) {
  return name.trim().toLowerCase();
}

function displayType(type?: string) {
  return type?.trim() || "?";
}

function schemaValueLabel(column: string, type?: string) {
  return `${column}: ${displayType(type)}`;
}
