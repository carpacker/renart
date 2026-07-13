import { expect, test } from "@playwright/test";

import {
  findPythonQueryLiterals,
  pythonQueryLiteralAtOffset,
  sourceOffsetForSQLOffset,
  sqlOffsetForSourceOffset,
} from "../../../lib/python-query-literals";

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
});
