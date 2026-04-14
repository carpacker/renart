"use client";

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
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { OnboardingConnectionFormProps } from "@/components/onboarding/types";

export function PostgresOnboardingForm({
  busy,
  defaultName,
  defaultValues,
  initialValues,
  onSubmit,
}: OnboardingConnectionFormProps) {
  const form = useForm({
    defaultValues: {
      host: String(initialValues?.host ?? defaultValues.host ?? "127.0.0.1"),
      port: String(initialValues?.port ?? defaultValues.port ?? 5432),
      username: String(initialValues?.username ?? defaultValues.username ?? "postgres"),
      password: String(initialValues?.password ?? defaultValues.password ?? ""),
      ssl_enabled:
        String(initialValues?.ssl_mode ?? defaultValues.ssl_mode ?? "disable") === "allow",
    },
    onSubmit: async ({ value }) => {
      await onSubmit({
        host: value.host.trim(),
        port: value.port.trim(),
        username: value.username.trim(),
        password: value.password,
        ssl_mode: value.ssl_enabled ? "allow" : "disable",
      });
    },
  });

  return (
    <Card className="rounded-2xl border shadow-sm">
      <CardHeader className="border-b px-6 py-5">
        <CardTitle className="text-xl font-semibold tracking-tight">Connect PostgreSQL</CardTitle>
        <CardDescription>
          We&apos;ll save this as `{defaultName}` after you validate and choose a database.
        </CardDescription>
      </CardHeader>
      <CardContent className="px-6 py-5">
        <form
          id="postgres-onboarding-form"
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          <form.Field
            name="host"
            validators={{
              onSubmit: ({ value }) => (!value.trim() ? "Host is required." : undefined),
            }}
            children={(field) => {
              const invalid = field.state.meta.isTouched && !field.state.meta.isValid;
              return (
                <div data-invalid={invalid || undefined} className="grid gap-1.5">
                  <Label htmlFor={field.name}>Host</Label>
                  <Input
                    id={field.name}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    aria-invalid={invalid}
                    placeholder="127.0.0.1"
                  />
                  {invalid ? <p className="text-xs text-destructive">{String(field.state.meta.errors[0])}</p> : null}
                </div>
              );
            }}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <form.Field
              name="port"
              validators={{
                onSubmit: ({ value }) => (!value.trim() ? "Port is required." : undefined),
              }}
              children={(field) => {
                const invalid = field.state.meta.isTouched && !field.state.meta.isValid;
                return (
                  <div data-invalid={invalid || undefined} className="grid gap-1.5">
                    <Label htmlFor={field.name}>Port</Label>
                    <Input
                      id={field.name}
                      type="number"
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(event.target.value)}
                      aria-invalid={invalid}
                      placeholder="5432"
                    />
                    {invalid ? <p className="text-xs text-destructive">{String(field.state.meta.errors[0])}</p> : null}
                  </div>
                );
              }}
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <form.Field
              name="username"
              validators={{
                onSubmit: ({ value }) => (!value.trim() ? "Username is required." : undefined),
              }}
              children={(field) => {
                const invalid = field.state.meta.isTouched && !field.state.meta.isValid;
                return (
                  <div data-invalid={invalid || undefined} className="grid gap-1.5">
                    <Label htmlFor={field.name}>Username</Label>
                    <Input
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(event.target.value)}
                      aria-invalid={invalid}
                      placeholder="postgres"
                    />
                    {invalid ? <p className="text-xs text-destructive">{String(field.state.meta.errors[0])}</p> : null}
                  </div>
                );
              }}
            />
            <form.Field
              name="password"
              children={(field) => (
                <div className="grid gap-1.5">
                  <Label htmlFor={field.name}>Password</Label>
                  <Input
                    id={field.name}
                    type="password"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    placeholder="postgres"
                  />
                </div>
              )}
            />
          </div>
          <form.Field
            name="ssl_enabled"
            children={(field) => (
              <FieldGroup>
                <FieldLabel htmlFor={field.name}>
                  <Field orientation="horizontal">
                    <FieldContent>
                      <FieldTitle>Allow SSL</FieldTitle>
                      <FieldDescription>
                        Turn this on if your Postgres server requires SSL. Leave it off for most local setups.
                      </FieldDescription>
                    </FieldContent>
                    <Switch
                      id={field.name}
                      checked={Boolean(field.state.value)}
                      onCheckedChange={(checked) => field.handleChange(Boolean(checked))}
                    />
                  </Field>
                </FieldLabel>
              </FieldGroup>
            )}
          />
        </form>
      </CardContent>
      <CardFooter className="flex justify-end border-t px-6 py-4">
        <Button type="submit" form="postgres-onboarding-form" disabled={busy}>
          Validate and continue
        </Button>
      </CardFooter>
    </Card>
  );
}
