"use client";

import { useEffect, useRef, useState } from "react";
import { X } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";

export function MultiValueInput({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string[];
  onChange: (value: string[]) => void;
  placeholder?: string;
  className?: string;
}) {
  const [draft, setDraft] = useState("");
  const [pendingAdds, setPendingAdds] = useState<string[]>([]);
  const [leavingValues, setLeavingValues] = useState<string[]>([]);
  const [optimisticRemovals, setOptimisticRemovals] = useState<string[]>([]);
  // Callers commonly provide `value ?? []`, so reference identity can change
  // during an unrelated workspace refresh. Only a semantic value change may
  // clear text the user is still entering.
  const valueKeyRef = useRef(JSON.stringify(value));
  useEffect(() => {
    const valueKey = JSON.stringify(value);
    if (valueKeyRef.current === valueKey) {
      return;
    }
    valueKeyRef.current = valueKey;
    setDraft("");
    setPendingAdds((current) => current.filter((item) => !value.includes(item)));
    setOptimisticRemovals((current) => current.filter((item) => value.includes(item)));
  }, [value]);

  const confirmedValues = value.filter(
    (item) => !optimisticRemovals.includes(item) || leavingValues.includes(item),
  );
  const visibleValues = [
    ...confirmedValues,
    ...pendingAdds.filter(
      (item) => !confirmedValues.includes(item) && !optimisticRemovals.includes(item),
    ),
    ...leavingValues.filter((item) => !confirmedValues.includes(item)),
  ];

  const addDraft = (raw = draft) => {
    const next = raw.trim();
    if (!next) {
      return;
    }
    if (!visibleValues.includes(next)) {
      setPendingAdds((current) => [...current, next]);
      onChange([...visibleValues, next]);
    }
    setDraft("");
  };
  const removeValue = (item: string) => {
    const nextPending = pendingAdds.filter((current) => current !== item);
    setPendingAdds(nextPending);
    if (value.includes(item)) {
      setLeavingValues((current) => (current.includes(item) ? current : [...current, item]));
      setOptimisticRemovals((current) => (current.includes(item) ? current : [...current, item]));
      window.setTimeout(() => {
        setLeavingValues((current) => current.filter((value) => value !== item));
      }, 180);
      window.setTimeout(() => {
        setOptimisticRemovals((current) => current.filter((value) => value !== item));
      }, 8000);
    }
    onChange([...value.filter((current) => current !== item), ...nextPending]);
  };

  return (
    <div
      className={cn(
        "dark:bg-input/30 border-input focus-within:border-ring focus-within:ring-ring/50 flex min-h-8 w-full min-w-0 flex-wrap items-center gap-1 rounded-lg border bg-transparent px-1.5 py-1 text-base transition-colors outline-none focus-within:ring-3 md:text-sm",
        className,
      )}
    >
      {visibleValues.map((item) => {
        const pending = pendingAdds.includes(item) && !value.includes(item);
        const leaving = leavingValues.includes(item);
        return (
          <Badge
            key={item}
            variant="outline"
            className={cn(
              "gap-1 pr-0.5 transition-all duration-150",
              leaving ? "scale-95 opacity-0" : "scale-100 opacity-100",
            )}
          >
            <span className="max-w-40 truncate">{item}</span>
            {pending ? (
              <Spinner className="size-3" />
            ) : (
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                className="-mr-0.5 size-4 rounded-full"
                aria-label={`Remove ${item}`}
                onClick={() => removeValue(item)}
              >
                <X />
              </Button>
            )}
          </Badge>
        );
      })}
      <Input
        className="h-6 min-w-24 flex-1 border-0 bg-transparent px-1 py-0 text-xs shadow-none focus-visible:ring-0"
        value={draft}
        placeholder={visibleValues.length === 0 ? placeholder : undefined}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={() => addDraft()}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            addDraft(event.currentTarget.value);
          } else if (
            event.key === "Backspace" &&
            event.currentTarget.value === "" &&
            value.length > 0
          ) {
            event.preventDefault();
            removeValue(value[value.length - 1]);
          }
        }}
      />
    </div>
  );
}
