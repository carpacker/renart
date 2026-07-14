"use client";

import { useEffect, useRef, useState } from "react";

import { FileText, Folder, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { getOnboardingPathSuggestions } from "@/lib/api-onboarding";
import type { IngestrSuggestion } from "@/lib/types";
import { cn } from "@/lib/utils";

type FilePathPickerProps = {
  value: string;
  placeholder?: string;
  onCommit: (value: string) => void;
  id?: string;
  ariaLabel?: string;
  variant?: "inline" | "field";
};

// FilePathPicker is the shared workspace-file combobox used by both the Load
// editor and creation dialogs. Selecting a directory drills into it; selecting
// a file, choosing the typed path, or pressing Enter commits the value.
export function FilePathPicker({
  value,
  placeholder,
  onCommit,
  id,
  ariaLabel,
  variant = "inline",
}: FilePathPickerProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState(value);
  const [suggestions, setSuggestions] = useState<IngestrSuggestion[]>([]);
  const [loading, setLoading] = useState(false);
  const requestRef = useRef(0);

  useEffect(() => {
    if (!open) return;
    const token = ++requestRef.current;
    setLoading(true);
    getOnboardingPathSuggestions(query)
      .then((result) => {
        if (token === requestRef.current) setSuggestions(result.suggestions ?? []);
      })
      .catch(() => {
        if (token === requestRef.current) setSuggestions([]);
      })
      .finally(() => {
        if (token === requestRef.current) setLoading(false);
      });
  }, [open, query]);

  const commit = (next: string) => {
    const trimmed = next.trim();
    if (trimmed) {
      onCommit(trimmed);
      setOpen(false);
    }
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) setQuery(value);
      }}
    >
      <PopoverTrigger asChild>
        {variant === "field" ? (
          <Button
            id={id}
            type="button"
            variant="outline"
            className="w-full justify-start"
            aria-label={ariaLabel}
          >
            <FileText data-icon="inline-start" />
            <span className="min-w-0 flex-1 truncate text-left">
              {value || placeholder || "Choose a file…"}
            </span>
          </Button>
        ) : (
          <button
            id={id}
            type="button"
            aria-label={ariaLabel}
            className={cn(
              "font-monaco flex min-w-0 flex-1 items-center gap-1 rounded-sm px-1 text-left outline-none hover:bg-muted/50 focus:bg-muted/60 focus:ring-1 focus:ring-ring",
              value ? "text-foreground" : "text-muted-foreground/60",
            )}
          >
            <FileText className="size-3 shrink-0 text-muted-foreground" />
            <span className="truncate">{value || placeholder || "path…"}</span>
          </button>
        )}
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 p-0">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder="Type a path…"
            className="text-xs"
            value={query}
            onValueChange={setQuery}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                commit(query);
              }
            }}
          />
          <CommandList>
            {loading ? (
              <div className="flex items-center gap-2 px-3 py-3 text-xs text-muted-foreground">
                <Loader2 className="size-3 animate-spin" /> listing…
              </div>
            ) : null}
            {!loading ? (
              <CommandEmpty className="py-3 text-xs">No matching paths.</CommandEmpty>
            ) : null}
            {query.trim() ? (
              <CommandGroup heading="Use path">
                <CommandItem
                  value={`__use__${query}`}
                  onSelect={() => commit(query)}
                  className="text-xs"
                >
                  <span className="flex-1 truncate">
                    Use “<span className="text-foreground">{query.trim()}</span>”
                  </span>
                </CommandItem>
              </CommandGroup>
            ) : null}
            {suggestions.length > 0 ? (
              <CommandGroup heading="Paths">
                {suggestions.map((suggestion) => {
                  const isDirectory = suggestion.kind === "directory";
                  return (
                    <CommandItem
                      key={suggestion.value}
                      value={suggestion.value}
                      onSelect={() => {
                        if (isDirectory) {
                          setQuery(suggestion.value);
                        } else {
                          commit(suggestion.value);
                        }
                      }}
                      className="text-xs"
                    >
                      {isDirectory ? <Folder /> : <FileText />}
                      <span className="flex-1 truncate">{suggestion.value}</span>
                    </CommandItem>
                  );
                })}
              </CommandGroup>
            ) : null}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
