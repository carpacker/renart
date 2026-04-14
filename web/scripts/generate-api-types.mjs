import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "..", "..");
const outputPath = resolve(repoRoot, "web", "lib", "generated", "api-types.ts");

const sources = [
  {
    file: resolve(repoRoot, "internal", "web", "service", "workspace_coordinator.go"),
    types: [
      "WorkspaceColumnCheck",
      "WorkspaceColumn",
      "WorkspaceAsset",
      "WorkspacePipeline",
      "WorkspaceState",
      "WorkspaceEvent",
    ],
  },
  {
    file: resolve(repoRoot, "internal", "web", "service", "config.go"),
    types: [
      "WorkspaceConfigFieldDef",
      "WorkspaceConfigConnectionType",
      "WorkspaceConfigConnection",
      "WorkspaceConfigEnvironment",
      "WorkspaceConfigResponse",
    ],
  },
  {
    file: resolve(repoRoot, "internal", "web", "service", "onboarding.go"),
    types: [
      "OnboardingImportFormState",
      "OnboardingImportResultState",
      "OnboardingSessionState",
      "OnboardingDiscoveryResult",
      "OnboardingPathSuggestionsResult",
    ],
  },
  {
    file: resolve(repoRoot, "internal", "web", "service", "suggestions.go"),
    types: ["SuggestionItem", "IngestrSuggestionsResult", "SQLPathSuggestionsResult"],
  },
  {
    file: resolve(repoRoot, "internal", "web", "service", "sql.go"),
    types: [
      "SQLDiscoveryTableItem",
      "SQLDatabaseDiscoveryResult",
      "SQLTableDiscoveryResult",
      "SQLTableColumnsResult",
      "SQLColumn",
    ],
  },
  {
    file: resolve(repoRoot, "internal", "web", "service", "parse_context.go"),
    types: [
      "ParseContextRange",
      "ParseContextPart",
      "ParseContextTable",
      "ParseContextColumn",
      "ParseContextDiagnostic",
      "ParseContextResult",
    ],
  },
  {
    file: resolve(repoRoot, "internal", "web", "service", "asset.go"),
    types: ["FormatSQLAssetResponse"],
  },
  {
    file: resolve(repoRoot, "internal", "web", "httpapi", "pipeline_execution.go"),
    types: ["PipelineMaterializationState", "PipelineMaterializationResponse"],
  },
  {
    file: resolve(repoRoot, "internal", "web", "model", "dto.go"),
    types: ["InspectResult", "InferColumnsResult"],
  },
];

const scalarMap = new Map([
  ["string", "string"],
  ["bool", "boolean"],
  ["int", "number"],
  ["int64", "number"],
  ["float64", "number"],
  ["time.Time", "string"],
  ["any", "unknown"],
]);

const renameMap = new Map([
  ["WorkspaceAsset", "WebAsset"],
  ["WorkspaceColumn", "WebColumn"],
  ["WorkspaceColumnCheck", "WebColumnCheck"],
  ["WorkspacePipeline", "WebPipeline"],
  ["OnboardingDiscoveryResult", "OnboardingDiscoveryResponse"],
  ["OnboardingPathSuggestionsResult", "OnboardingPathSuggestionsResponse"],
  ["SuggestionItem", "IngestrSuggestion"],
  ["IngestrSuggestionsResult", "IngestrSuggestionsResponse"],
  ["SQLPathSuggestionsResult", "SqlPathSuggestionsResponse"],
  ["SQLDatabaseDiscoveryResult", "SqlDiscoveryDatabasesResponse"],
  ["SQLTableDiscoveryResult", "SqlDiscoveryTablesResponse"],
  ["SQLTableColumnsResult", "SqlDiscoveryTableColumnsResponse"],
  ["SQLDiscoveryTableItem", "SqlDiscoveryTable"],
  ["ParseContextRange", "SqlParseContextRange"],
  ["ParseContextPart", "SqlParseContextPart"],
  ["ParseContextTable", "SqlParseContextTable"],
  ["ParseContextColumn", "SqlParseContextColumn"],
  ["ParseContextDiagnostic", "SqlParseContextDiagnostic"],
  ["ParseContextResult", "SqlParseContextResponse"],
  ["InspectResult", "AssetInspectResponse"],
  ["InferColumnsResult", "InferColumnsResponse"],
  ["Column", "WebColumn"],
]);

function splitFields(body) {
  return body
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("//"));
}

function extractStructBody(content, typeName) {
  const typeIndex = content.indexOf(`type ${typeName} struct {`);
  if (typeIndex < 0) {
    throw new Error(`Type ${typeName} not found`);
  }

  const bodyStart = content.indexOf("{", typeIndex) + 1;
  let depth = 1;
  let index = bodyStart;
  while (index < content.length && depth > 0) {
    const char = content[index];
    if (char === "{") depth += 1;
    if (char === "}") depth -= 1;
    index += 1;
  }

  return content.slice(bodyStart, index - 1);
}

function jsonNameFromTag(tag, fieldName) {
  const tagMatch = tag?.match(/json:"([^,"]+)/);
  if (tagMatch?.[1]) {
    return tagMatch[1];
  }

  return fieldName
    .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
    .toLowerCase();
}

function isOptionalTag(tag) {
  return Boolean(tag?.includes(",omitempty"));
}

function goTypeToTs(goType) {
  let value = goType.trim();
  if (value.startsWith("[]")) {
    return `${goTypeToTs(value.slice(2))}[]`;
  }
  if (value.startsWith("map[")) {
    const match = value.match(/^map\[[^\]]+\](.+)$/);
    return `Record<string, ${goTypeToTs(match[1])}>`;
  }
  if (value.startsWith("*")) {
    return goTypeToTs(value.slice(1));
  }

  return renameMap.get(value) ?? scalarMap.get(value) ?? value;
}

function parseField(line) {
  const fieldMatch = line.match(/^(\w+)\s+([^`]+?)(?:\s+`([^`]*)`)?$/);
  if (!fieldMatch) {
    return null;
  }

  const [, fieldName, rawType, rawTag] = fieldMatch;
  return {
    fieldName,
    propertyName: jsonNameFromTag(rawTag, fieldName),
    tsType: goTypeToTs(rawType),
    optional: isOptionalTag(rawTag),
  };
}

function renderType(typeName, fields) {
  const mappedName = renameMap.get(typeName) ?? typeName;

  const body = fields
    .map((field) => `  ${field.propertyName}${field.optional ? "?" : ""}: ${field.tsType};`)
    .join("\n");

  return `export type ${mappedName} = {\n${body}\n};`;
}

const blocks = [];
for (const source of sources) {
  const content = await readFile(source.file, "utf8");
  for (const typeName of source.types) {
    const body = extractStructBody(content, typeName);
    const fields = splitFields(body)
      .map(parseField)
      .filter(Boolean);
    blocks.push(renderType(typeName, fields));
  }
}

const output = `// Code generated by web/scripts/generate-api-types.mjs. DO NOT EDIT.\n\n${blocks.join("\n\n")}\n`;
await mkdir(dirname(outputPath), { recursive: true });
await writeFile(outputPath, output, "utf8");
