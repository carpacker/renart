import { expect, test } from "@playwright/test";

import { detectClipboardSeedFormat, prepareClipboardSeed } from "../../../lib/seed-clipboard";

test.describe("seed clipboard import", () => {
  test("detects the supported clipboard shapes", () => {
    expect(detectClipboardSeedFormat('[{"id":1}]')).toBe("json");
    expect(detectClipboardSeedFormat('{"id":1}\n{"id":2}\n')).toBe("jsonl");
    expect(detectClipboardSeedFormat("id\tname\n1\tAda\n")).toBe("tsv");
    expect(detectClipboardSeedFormat("id,name\n1,Ada\n")).toBe("csv");
    expect(detectClipboardSeedFormat("Ada\nGrace\n")).toBe("text");
  });

  test("converts TSV and plain text to executable CSV seeds", () => {
    const tsv = prepareClipboardSeed('id\tname\n1\t"Ada ""Lovelace"""\n', "auto");
    expect(tsv.fileType).toBe("csv");
    expect(tsv.fileName).toBe("pasted-seed.csv");
    expect(tsv.content).toBe('id,name\n1,"Ada ""Lovelace"""\n');

    const text = prepareClipboardSeed("Ada\nGrace, Hopper\n", "text");
    expect(text.fileType).toBe("csv");
    expect(text.content).toBe('value\nAda\n"Grace, Hopper"\n');

    const named = prepareClipboardSeed("id,name\n1,Ada\n", "csv", "Customer List");
    expect(named.fileName).toBe("Customer-List.csv");
  });

  test("validates an explicit format override", () => {
    expect(() => prepareClipboardSeed("not json", "json")).toThrow("not valid JSON");
    expect(prepareClipboardSeed("name\nAda\n", "text").inputFormat).toBe("text");
  });
});
