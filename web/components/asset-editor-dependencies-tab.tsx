"use client";

import { useEffect, useMemo, useState } from "react";

import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxValue,
  useComboboxAnchor,
} from "@/components/ui/combobox";

type AssetEditorDependenciesTabProps = {
  assetName?: string;
  manualUpstreamNames: string[];
  inferredUpstreamNames: string[];
  availableDependencyNames: string[];
  disabled?: boolean;
  onSaveManualUpstreams: (upstreams: string[]) => void;
};

export function AssetEditorDependenciesTab({
  assetName,
  manualUpstreamNames,
  inferredUpstreamNames,
  availableDependencyNames,
  disabled = false,
  onSaveManualUpstreams,
}: AssetEditorDependenciesTabProps) {
  const normalizedOptions = useMemo(
    () => compactUnique(availableDependencyNames).filter((name) => name !== assetName),
    [assetName, availableDependencyNames]
  );
  const [manualDraft, setManualDraft] = useState<string[]>(manualUpstreamNames);

  useEffect(() => {
    setManualDraft(manualUpstreamNames);
  }, [manualUpstreamNames]);

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <div className="text-sm font-medium">Manual dependencies</div>
        <DependencyMultiSelect
          availableDependencyNames={normalizedOptions}
          disabled={disabled}
          value={manualDraft}
          onChange={(nextValue) => {
            setManualDraft(nextValue);
            onSaveManualUpstreams(nextValue);
          }}
        />
        <p className="text-xs text-muted-foreground">
          Only manually managed dependencies are editable here. SQL-inferred dependencies
          are tracked automatically.
        </p>
      </div>

      <div className="space-y-2 rounded-md border bg-muted/20 p-3">
        <div className="text-sm font-medium">Automatically inferred</div>
        {inferredUpstreamNames.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {inferredUpstreamNames.map((name) => (
              <span
                key={name}
                className="rounded-full border px-2 py-1 text-xs text-muted-foreground"
              >
                {name}
              </span>
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">
            No automatically inferred dependencies for this asset.
          </p>
        )}
      </div>
    </div>
  );
}

function DependencyMultiSelect({
  value,
  onChange,
  disabled,
  availableDependencyNames,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  disabled: boolean;
  availableDependencyNames: string[];
}) {
  const anchor = useComboboxAnchor();
  const normalizedValue = compactUnique(value);
  const [draft, setDraft] = useState("");

  const items = useMemo(() => {
    const draftValue = draft.trim();
    if (!draftValue || availableDependencyNames.includes(draftValue)) {
      return availableDependencyNames;
    }
    return [...availableDependencyNames, draftValue];
  }, [availableDependencyNames, draft]);

  const toggleValue = (item: string) => {
    const alreadySelected = normalizedValue.some(
      (current) => current.toLowerCase() === item.toLowerCase()
    );

    if (alreadySelected) {
      onChange(normalizedValue.filter((current) => current.toLowerCase() !== item.toLowerCase()));
      setDraft("");
      return;
    }

    onChange(compactUnique([...normalizedValue, item]));
    setDraft("");
  };

  const commitDraft = () => {
    const additions = draft
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
    if (additions.length === 0) {
      return;
    }

    onChange(compactUnique([...normalizedValue, ...additions]));
    setDraft("");
  };

  const handleValueChange = (nextValue: string[] | string) => {
    setDraft("");
    onChange(Array.isArray(nextValue) ? compactUnique(nextValue as string[]) : []);
  };

  return (
    <Combobox
      multiple
      autoHighlight
      items={items}
      onValueChange={handleValueChange}
      value={normalizedValue}
    >
      <ComboboxChips ref={anchor} className="w-full">
        <ComboboxValue>
          {(values) => (
            <>
              {(values as string[]).map((name) => (
                <ComboboxChip key={name}>{name}</ComboboxChip>
              ))}
              <ComboboxChipsInput
                value={draft}
                placeholder="Add dependency"
                disabled={disabled}
                onChange={(event) => setDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === ",") {
                    event.preventDefault();
                    commitDraft();
                  }
                }}
              />
            </>
          )}
        </ComboboxValue>
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxEmpty>No dependencies found.</ComboboxEmpty>
        <ComboboxList>
          {(item) => (
            <ComboboxItem
              key={item}
              value={item}
              onClick={() => {
                toggleValue(item);
              }}
            >
              {item}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

function compactUnique(values: string[]): string[] {
  const seen = new Set<string>();
  const result: string[] = [];

  for (const value of values) {
    const trimmed = value.trim();
    if (!trimmed) {
      continue;
    }

    const key = trimmed.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    result.push(trimmed);
  }

  return result;
}
