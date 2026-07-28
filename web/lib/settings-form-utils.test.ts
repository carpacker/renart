import { describe, expect, it } from "vitest";

import {
  buildConnectionFieldDefaults,
  connectionSecretsReady,
  splitConnectionDraftValues,
} from "@/lib/settings-form-utils";
import type { WorkspaceConfigConnectionType } from "@/lib/types";

const postgresType: WorkspaceConfigConnectionType = {
  type_name: "postgres",
  category: "warehouse",
  fields: [
    {
      name: "host",
      type: "string",
      is_required: true,
      is_sensitive: false,
      is_sensitive_file: false,
    },
    {
      name: "password",
      type: "string",
      is_required: true,
      is_sensitive: true,
      is_sensitive_file: false,
    },
  ],
};

describe("connection secret form helpers", () => {
  it("keeps sensitive defaults out of ordinary form values", () => {
    const values = buildConnectionFieldDefaults({
      connectionTypes: [postgresType],
      existingConnection: {
        name: "warehouse",
        type: "postgres",
        values: { host: "db.internal", password: "must-not-enter-browser-state" },
        secret_fields: {
          password: { status: "configured", writable: true, rotatable: true },
        },
      },
      typeName: "postgres",
    });

    expect(values).toEqual({ host: "db.internal" });
  });

  it("partitions a draft into public values and write-only replacements", () => {
    expect(
      splitConnectionDraftValues(postgresType, {
        host: "db.internal",
        password: "write-only",
      }),
    ).toEqual({
      values: { host: "db.internal" },
      secretChanges: {
        password: { action: "replace", value: "write-only" },
      },
    });
  });

  it("turns onboarding environment secret fields into references without exposing values", () => {
    expect(
      splitConnectionDraftValues(
        postgresType,
        {
          host: "db.internal",
          password: "WAREHOUSE_PASSWORD",
        },
        { password: "env" },
      ),
    ).toEqual({
      values: { host: "db.internal" },
      secretChanges: {
        password: {
          action: "replace",
          binding: { ref: "env:WAREHOUSE_PASSWORD" },
        },
      },
    });
  });

  it("accepts keep only when a required secret is already configured", () => {
    expect(
      connectionSecretsReady({
        connection: {
          name: "warehouse",
          type: "postgres",
          values: { host: "db.internal" },
          secret_fields: {
            password: { status: "configured", writable: true, rotatable: true },
          },
        },
        connectionType: postgresType,
        secretChanges: { password: { action: "keep" } },
      }),
    ).toBe(true);
    expect(
      connectionSecretsReady({
        connection: null,
        connectionType: postgresType,
        secretChanges: { password: { action: "keep" } },
      }),
    ).toBe(false);
  });

  it("accepts a valid environment binding without putting its value in form state", () => {
    expect(
      connectionSecretsReady({
        connection: null,
        connectionType: postgresType,
        secretChanges: {
          password: {
            action: "replace",
            binding: { ref: "env:WAREHOUSE_PASSWORD" },
          },
        },
      }),
    ).toBe(true);
    expect(
      connectionSecretsReady({
        connection: null,
        connectionType: postgresType,
        secretChanges: {
          password: {
            action: "replace",
            binding: { ref: "env:" },
          },
        },
      }),
    ).toBe(false);
  });

  it("allows edits to keep a tracked reference even when it is unavailable locally", () => {
    expect(
      connectionSecretsReady({
        connection: {
          name: "warehouse",
          type: "postgres",
          values: { host: "db.internal" },
          secret_fields: {
            password: {
              status: "missing",
              provider: "env",
              reference: "env:WAREHOUSE_PASSWORD",
              writable: false,
              rotatable: false,
            },
          },
        },
        connectionType: postgresType,
        secretChanges: { password: { action: "keep" } },
      }),
    ).toBe(true);
  });
});
