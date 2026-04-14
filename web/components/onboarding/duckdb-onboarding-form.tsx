"use client";

import { useEffect, useMemo, useState } from "react";
import { useForm } from "@tanstack/react-form";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox";
import { Label } from "@/components/ui/label";
import { OnboardingConnectionFormProps } from "@/components/onboarding/types";
import { getOnboardingPathSuggestions } from "@/lib/api";

export function DuckDBOnboardingForm({
  busy,
  defaultName,
  defaultValues,
  initialValues,
  onSubmit,
}: OnboardingConnectionFormProps) {
  const initialPath = String(initialValues?.path ?? defaultValues.path ?? "./duckdb-files/local.db");
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const [suggestionError, setSuggestionError] = useState<string | null>(null);
  const [pathValue, setPathValue] = useState(initialPath);
  const form = useForm({
    defaultValues: {
      path: initialPath,
      read_only: Boolean(initialValues?.read_only ?? defaultValues.read_only ?? false),
    },
    onSubmit: async ({ value }) => {
      await onSubmit({
        path: value.path.trim(),
        read_only: value.read_only,
      });
    },
  });

  useEffect(() => {
    let cancelled = false;
    const loadSuggestions = async () => {
      try {
        const response = await getOnboardingPathSuggestions(pathValue || "./");
        if (cancelled) {
          return;
        }
        setSuggestions(response.suggestions.map((item) => item.value));
        setSuggestionError(null);
      } catch (error) {
        if (cancelled) {
          return;
        }
        setSuggestions([]);
        setSuggestionError(error instanceof Error ? error.message : "Failed to load file suggestions.");
      }
    };

    void loadSuggestions();
    return () => {
      cancelled = true;
    };
  }, [pathValue]);

  const items = useMemo(() => {
    const trimmed = pathValue.trim();
    if (!trimmed || suggestions.includes(trimmed)) {
      return suggestions;
    }
    return [trimmed, ...suggestions];
  }, [pathValue, suggestions]);

  return (
    <Card className="rounded-2xl border shadow-sm">
      <CardHeader className="border-b px-6 py-5">
        <CardTitle className="text-xl font-semibold tracking-tight">Connect DuckDB</CardTitle>
        <CardDescription>
          Pick the local DuckDB file to use. We&apos;ll save this as `{defaultName}`.
        </CardDescription>
      </CardHeader>
      <CardContent className="px-6 py-5">
        <form
          id="duckdb-onboarding-form"
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          <form.Field
            name="path"
            validators={{
              onSubmit: ({ value }) => (!value.trim() ? "DuckDB file path is required." : undefined),
            }}
            children={(field) => {
              const invalid = field.state.meta.isTouched && !field.state.meta.isValid;
              return (
                <div data-invalid={invalid || undefined} className="grid gap-1.5">
                  <Label htmlFor={field.name}>DuckDB file</Label>
                    <Combobox
                      items={items}
                     onValueChange={(nextValue) => {
                       const nextPath = nextValue ?? "";
                       setPathValue(nextPath);
                       field.handleChange(nextPath);
                     }}
                     value={pathValue}
                   >
                     <ComboboxInput
                       value={pathValue}
                       onBlur={field.handleBlur}
                       onChange={(event) => {
                         const nextPath = event.target.value;
                         setPathValue(nextPath);
                         field.handleChange(nextPath);
                       }}
                       placeholder="./duckdb-files/local.db"
                       aria-invalid={invalid}
                     />
                     <ComboboxContent>
                       <ComboboxEmpty>No matching files.</ComboboxEmpty>
                        <ComboboxList>
                         {(item) => (
                           <ComboboxItem
                             key={item}
                             value={item}
                             onClick={() => {
                               setPathValue(item);
                               field.handleChange(item);
                             }}
                           >
                             {item}
                           </ComboboxItem>
                         )}
                      </ComboboxList>
                     </ComboboxContent>
                   </Combobox>
                  <p className="text-xs text-muted-foreground">
                    Relative paths are resolved from the workspace. Use an existing `.db` or `.duckdb` file.
                  </p>
                  {suggestionError ? <p className="text-xs text-destructive">{suggestionError}</p> : null}
                  {invalid ? <p className="text-xs text-destructive">{String(field.state.meta.errors[0])}</p> : null}
                </div>
              );
            }}
          />
          <form.Field
            name="read_only"
            children={(field) => (
              <label className="flex items-center gap-2 text-sm font-medium">
                <Checkbox
                  checked={Boolean(field.state.value)}
                  onCheckedChange={(checked) => field.handleChange(Boolean(checked))}
                />
                Open in read-only mode
              </label>
            )}
          />
        </form>
      </CardContent>
      <CardFooter className="flex justify-end border-t px-6 py-4">
        <Button type="submit" form="duckdb-onboarding-form" disabled={busy}>
          Validate and continue
        </Button>
      </CardFooter>
    </Card>
  );
}
