import { expect, test } from "@playwright/test";

import {
  findPythonQueryLiterals,
  pythonQueryLiteralAtOffset,
  sourceOffsetForSQLOffset,
  sqlOffsetForSourceOffset,
} from "../../../lib/python-query-literals";
import { computeMinimalTextEdit, transformOffsetThroughEdit } from "../../../lib/monaco-model-sync";

test.describe("Python query literal projection", () => {
  test("extracts stable query strings and ignores dynamic or non-code lookalikes", () => {
    const source = [
      '# query("select * from commented")',
      'text = "query(\\"select * from a string\\")"',
      "dynamic = query(sql)",
      'formatted = query(f"select * from {table}")',
      'concatenated = query("select * from " + table)',
      'transformed = query("select 1".strip())',
      'bytes_sql = query(b"select 1")',
      'invalid_prefix = query(rr"select 1")',
      'other = client.query("select 1")',
      'spaced_other = client . query("select 1")',
      'first = query("select * from analytics.orders")',
      "second = renart . query(r'''select * from analytics.customers''', format='pandas')",
    ].join("\n");

    expect(findPythonQueryLiterals(source).map((literal) => literal.sql)).toEqual([
      "select * from analytics.orders",
      "select * from analytics.customers",
    ]);
  });

  test("maps decoded SQL offsets back into the Python source", () => {
    const source = 'result = query("select\\norder_id from analytics.orders")';
    const [literal] = findPythonQueryLiterals(source);
    expect(literal.sql).toBe("select\norder_id from analytics.orders");

    const sqlOrderOffset = literal.sql.indexOf("order_id");
    const sourceOrderOffset = source.indexOf("order_id");
    expect(sourceOffsetForSQLOffset(literal, sqlOrderOffset)).toBe(sourceOrderOffset);
    expect(sqlOffsetForSourceOffset(literal, sourceOrderOffset)).toBe(sqlOrderOffset);
    expect(pythonQueryLiteralAtOffset(source, sourceOrderOffset)?.sql).toBe(literal.sql);
  });

  test("keeps an unfinished plain query literal projected while typing", () => {
    const source = 'result = query("select * from analytics.orders as o where o.';
    const [literal] = findPythonQueryLiterals(source);

    expect(literal.sql).toBe("select * from analytics.orders as o where o.");
    expect(literal.sourceEnd).toBe(source.length);
    expect(pythonQueryLiteralAtOffset(source, source.length)).toEqual(literal);
  });

  test("extracts static query connection overrides", () => {
    const source = [
      'first = query("select * from analytics.orders", connection="postgres-default")',
      'second = renart.query("select 1", r"duckdb-default", format="arrow")',
      'third = query("select 2", format="pandas", connection = "snowflake-prod")',
      'dynamic = query("select 3", connection=connection_name)',
    ].join("\n");

    expect(
      findPythonQueryLiterals(source).map((literal) => ({
        sql: literal.sql,
        connection: literal.connection,
      })),
    ).toEqual([
      { sql: "select * from analytics.orders", connection: "postgres-default" },
      { sql: "select 1", connection: "duckdb-default" },
      { sql: "select 2", connection: "snowflake-prod" },
      { sql: "select 3", connection: undefined },
    ]);
  });
});

test.describe("Monaco external model synchronization", () => {
  test("reduces a snapshot to a cursor-transformable edit", () => {
    const edit = computeMinimalTextEdit("select amount from orders", "select total from orders");
    expect(edit).toEqual({ start: 7, end: 13, text: "total" });
    expect(transformOffsetThroughEdit(3, edit!)).toBe(3);
    expect(transformOffsetThroughEdit(10, edit!)).toBe(12);
    expect(transformOffsetThroughEdit(25, edit!)).toBe(24);
  });
});
