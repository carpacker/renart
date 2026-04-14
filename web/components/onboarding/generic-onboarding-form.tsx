"use client";

import { useMemo } from "react";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { OnboardingConnectionFormProps } from "@/components/onboarding/types";

export function GenericOnboardingForm({
  busy,
  defaultName,
  defaultValues,
  initialValues,
  onSubmit,
}: OnboardingConnectionFormProps) {
  const seeded = useMemo(
    () => ({ ...defaultValues, ...initialValues }),
    [defaultValues, initialValues]
  );
  const keys = Object.keys(seeded);
  const form = useForm({
    defaultValues: keys.reduce<Record<string, string>>((result, key) => {
      result[key] = String(seeded[key] ?? "");
      return result;
    }, {}),
    onSubmit: async ({ value }) => {
      await onSubmit(value);
    },
  });

  return (
    <Card className="rounded-2xl border shadow-sm">
      <CardHeader className="border-b px-6 py-5">
        <CardTitle className="text-xl font-semibold tracking-tight">Connect warehouse</CardTitle>
        <CardDescription>
          We&apos;ll save this as `{defaultName}` after validation.
        </CardDescription>
      </CardHeader>
      <CardContent className="px-6 py-5">
        <form
          id="generic-onboarding-form"
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          {keys.map((key) => (
            <form.Field
              key={key}
              name={key}
              validators={{
                onSubmit: ({ value }) => (!String(value).trim() ? `${humanize(key)} is required.` : undefined),
              }}
              children={(field) => {
                const invalid = field.state.meta.isTouched && !field.state.meta.isValid;
                return (
                  <div data-invalid={invalid || undefined} className="grid gap-1.5">
                    <Label htmlFor={field.name}>{humanize(key)}</Label>
                    <Input
                      id={field.name}
                      type={secretInputType(key)}
                      value={String(field.state.value ?? "")}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(event.target.value)}
                      aria-invalid={invalid}
                    />
                    {invalid ? <p className="text-xs text-destructive">{String(field.state.meta.errors[0])}</p> : null}
                  </div>
                );
              }}
            />
          ))}
        </form>
      </CardContent>
      <CardFooter className="flex justify-end border-t px-6 py-4">
        <Button type="submit" form="generic-onboarding-form" disabled={busy}>
          Validate and continue
        </Button>
      </CardFooter>
    </Card>
  );
}

function humanize(value: string) {
  return value.replaceAll("_", " ").replace(/\b\w/g, (char) => char.toUpperCase());
}

function secretInputType(name: string) {
  const lower = name.toLowerCase();
  return ["password", "secret", "token", "key"].some((part) => lower.includes(part))
    ? "password"
    : "text";
}
