import json
import sys

from sqlglot import exp, parse
from sqlglot.optimizer.scope import find_all_in_scope, traverse_scope


def extract_tables(parsed):
    if parsed is None:
        return []

    return list(parsed.find_all(exp.Table))


def get_table_name(table):
    db_name = table.catalog + "." if hasattr(table, "catalog") and table.catalog else ""
    schema_name = table.db + "." if hasattr(table, "db") and table.db else ""

    table_name = table.name
    if not table_name and hasattr(table, "this"):
        if isinstance(table.this, exp.Anonymous):
            if isinstance(table.this.this, exp.Identifier):
                table_name = table.this.this.name
            else:
                table_name = str(table.this.this)
        elif isinstance(table.this, exp.Identifier):
            table_name = table.this.this

    return db_name + schema_name + table_name


def get_table_name_with_context(table, current_database):
    if hasattr(table, "catalog") and table.catalog:
        db_name = table.catalog + "."
        schema_name = (table.db + ".") if hasattr(table, "db") and table.db else "dbo."
    else:
        db_name = current_database + "."
        schema_name = (table.db + ".") if hasattr(table, "db") and table.db else "dbo."

    table_name = table.name
    if not table_name and hasattr(table, "this"):
        if isinstance(table.this, exp.Anonymous):
            if isinstance(table.this.this, exp.Identifier):
                table_name = table.this.this.name
            else:
                table_name = str(table.this.this)
        elif isinstance(table.this, exp.Identifier):
            table_name = table.this.this

    return db_name + schema_name + table_name


def offset_to_line_col(query, offset):
    if offset < 0:
        return (1, 1)

    bounded_offset = min(offset, len(query))
    line = 1
    last_line_start = 0

    for index, char in enumerate(query[:bounded_offset]):
        if char == "\n":
            line += 1
            last_line_start = index + 1

    return (line, bounded_offset - last_line_start + 1)


def build_range_from_meta(query, meta):
    start = meta.get("start")
    end = meta.get("end")
    if start is None or end is None:
        return None

    start_line, start_col = offset_to_line_col(query, start)
    end_line, end_col = offset_to_line_col(query, end + 1)
    return {
        "start": start,
        "end": end + 1,
        "line": start_line,
        "col": start_col,
        "end_line": end_line,
        "end_col": end_col,
    }


def build_identifier_parts(query, parts, last_kind):
    result = []
    total = len(parts)

    for index, part in enumerate(parts):
        if not hasattr(part, "meta"):
            continue

        range_info = build_range_from_meta(query, part.meta)
        if not range_info:
            continue

        name = part.name if hasattr(part, "name") else str(part)
        kind = last_kind if index == total - 1 else "schema"
        result.append({"name": name, "kind": kind, "range": range_info})

    return result


def build_column_parts(query, column):
    result = []
    parts = column.parts
    total = len(parts)

    for index, part in enumerate(parts):
        if not hasattr(part, "meta"):
            continue

        range_info = build_range_from_meta(query, part.meta)
        if not range_info:
            continue

        name = part.name if hasattr(part, "name") else str(part)
        if index == total - 1:
            kind = "column"
        elif index == total - 2:
            kind = "table"
        else:
            kind = "schema"

        result.append({"name": name, "kind": kind, "range": range_info})

    return result


def build_alias_range(query, table):
    alias_expr = table.args.get("alias")
    if alias_expr is None:
        return None

    alias_identifier = alias_expr.this if hasattr(alias_expr, "this") else alias_expr
    if alias_identifier is None or not hasattr(alias_identifier, "meta"):
        return None

    return build_range_from_meta(query, alias_identifier.meta)


def paren_depth_at(query, offset):
    depth = 0
    index = 0
    in_single_quote = False
    in_double_quote = False

    while index < min(offset, len(query)):
        char = query[index]
        if char == "'" and not in_double_quote:
            if in_single_quote and index + 1 < len(query) and query[index + 1] == "'":
                index += 2
                continue
            in_single_quote = not in_single_quote
        elif char == '"' and not in_single_quote:
            in_double_quote = not in_double_quote
        elif not in_single_quote and not in_double_quote:
            if char == "(":
                depth += 1
            elif char == ")" and depth > 0:
                depth -= 1
        index += 1

    return depth


def find_previous_keyword_at_depth(query, offset, keyword, depth):
    lowered = query.lower()
    keyword = keyword.lower()
    index = offset

    while index >= 0:
        found = lowered.rfind(keyword, 0, index)
        if found < 0:
            return -1

        before = lowered[found - 1] if found > 0 else " "
        after_index = found + len(keyword)
        after = lowered[after_index] if after_index < len(lowered) else " "
        if (before.isalnum() or before == "_") or (after.isalnum() or after == "_"):
            index = found
            continue

        if paren_depth_at(query, found) == depth:
            return found

        index = found

    return -1


def build_table_scope_range(query, table):
    table_range = build_range_from_meta(query, table.meta) if hasattr(table, "meta") else None
    if not table_range:
        part_ranges = [
            build_range_from_meta(query, part.meta)
            for part in getattr(table, "parts", [])
            if hasattr(part, "meta")
        ]
        part_ranges = [range_info for range_info in part_ranges if range_info]
        if part_ranges:
            table_range = {
                "start": part_ranges[0]["start"],
                "end": part_ranges[-1]["end"],
                "line": part_ranges[0]["line"],
                "col": part_ranges[0]["col"],
                "end_line": part_ranges[-1]["end_line"],
                "end_col": part_ranges[-1]["end_col"],
            }
    if not table_range:
        return None

    depth = paren_depth_at(query, table_range["start"])
    source_keyword_starts = [
        find_previous_keyword_at_depth(query, table_range["start"], keyword, depth)
        for keyword in ("from", "join", "update", "into")
    ]
    source_keyword_starts = [start for start in source_keyword_starts if start >= 0]
    source_start = max(source_keyword_starts) if source_keyword_starts else table_range["start"]
    select_start = find_previous_keyword_at_depth(query, source_start, "select", depth)
    start = select_start if select_start >= 0 else source_start
    start_line, start_col = offset_to_line_col(query, start)

    return {
        "start": start,
        "end": table_range["end"],
        "line": start_line,
        "col": start_col,
        "end_line": table_range["end_line"],
        "end_col": table_range["end_col"],
    }


def build_expression_alias_range(query, expression):
    alias_expr = expression.args.get("alias") if hasattr(expression, "args") else None
    if alias_expr is None:
        return None

    for alias_identifier in (alias_expr, alias_expr.this if hasattr(alias_expr, "this") else None):
        if alias_identifier is None or not hasattr(alias_identifier, "meta"):
            continue

        range_info = build_range_from_meta(query, alias_identifier.meta)
        if range_info:
            return range_info

    return None


def detect_table_source_kind(table):
    table_this = getattr(table, "this", None)
    if isinstance(table_this, exp.Anonymous) or isinstance(table_this, exp.Func):
        return "table_function"
    return "table"


def is_cte_table_reference(table, cte_schemas):
    return (
        table.name in cte_schemas
        and not getattr(table, "db", None)
        and not getattr(table, "catalog", None)
    )


def is_inside_cte_definition(table):
    return table.find_ancestor(exp.CTE) is not None


def get_table_display_name(table, current_database, dialect):
    source_kind = detect_table_source_kind(table)
    if source_kind == "table_function":
        table_this = getattr(table, "this", None)
        if isinstance(table_this, exp.Anonymous):
            return str(table_this.this or "")
        if table_this is not None:
            return str(getattr(table_this, "this", "") or table_this)
        return ""

    if dialect == "tsql" and current_database:
        return get_table_name_with_context(table, current_database)

    return get_table_name(table)


def normalize_identifier(value):
    return value.strip().strip('"`[]').lower()


def build_schema_lookups(schema):
    exact = {}
    short = {}

    if not schema:
        return exact, short

    for table_name, columns in schema.items():
        normalized_name = normalize_identifier(table_name)
        exact[normalized_name] = {
            "name": table_name,
            "columns": {normalize_identifier(column_name) for column_name in columns.keys()},
        }
        short_name = normalize_identifier(table_name.split(".")[-1])
        short.setdefault(short_name, []).append(table_name)

    return exact, short


MATERIALIZED_COLUMN_METHODS = {"connection-column-discovery", "materialized-workspace-load"}
DEFINITION_COLUMN_METHODS = {"workspace-load", "workspace-event", "asset-column-inference", "asset-sql-definition"}


def schema_columns_for_table(table_name, schema, cte_schemas=None):
    cte_schemas = cte_schemas or {}
    combined_schema = dict(schema or {})
    combined_schema.update(cte_schemas)
    exact_schema, short_schema = build_schema_lookups(combined_schema)
    resolved_name = resolve_table_name_from_schema(table_name, exact_schema, short_schema)
    if not resolved_name:
        return None

    return combined_schema.get(resolved_name, {})


def schema_column_metadata_for_table(table_name, schema, column_source_methods, cte_schemas=None, cte_column_metadata=None):
    cte_schemas = cte_schemas or {}
    cte_column_metadata = cte_column_metadata or {}
    combined_schema = dict(schema or {})
    combined_schema.update(cte_schemas)
    exact_schema, short_schema = build_schema_lookups(combined_schema)
    resolved_name = resolve_table_name_from_schema(table_name, exact_schema, short_schema)
    if not resolved_name:
        return {}

    if resolved_name in cte_column_metadata:
        return cte_column_metadata.get(resolved_name, {})

    columns = combined_schema.get(resolved_name, {})
    source_methods_by_column = (column_source_methods or {}).get(resolved_name, {})
    table_actual_schema_known = any(
        bool(set(methods or []) & MATERIALIZED_COLUMN_METHODS)
        for methods in source_methods_by_column.values()
    )
    return {
        column_name: {
            "source_methods": source_methods_by_column.get(column_name, []),
            "origin_table": resolved_name,
            "actual_schema_known": table_actual_schema_known,
        }
        for column_name in columns.keys()
    }


def collect_scope_output_columns(query, select_expression, schema, column_source_methods, cte_schemas, cte_column_metadata):
    columns = {}
    column_ranges = {}
    column_metadata = {}

    if not isinstance(select_expression, exp.Select):
        return columns, column_ranges, column_metadata

    for expression in select_expression.expressions:
        if isinstance(expression, exp.Star):
            for table in extract_tables(select_expression):
                if is_inside_cte_definition(table) and table.find_ancestor(exp.CTE) is not select_expression.find_ancestor(exp.CTE):
                    continue
                table_name = get_table_name(table)
                source_columns = schema_columns_for_table(table_name, schema, cte_schemas)
                if source_columns:
                    columns.update(source_columns)
                    column_metadata.update(schema_column_metadata_for_table(table_name, schema, column_source_methods, cte_schemas, cte_column_metadata))
            continue

        name = expression.alias_or_name
        if name:
            columns[name] = ""
            column_metadata[name] = {"source_methods": ["query-expression"], "origin_table": ""}
            alias_range = build_expression_alias_range(query, expression)
            if alias_range:
                column_ranges[name] = alias_range

    return columns, column_ranges, column_metadata


def collect_cte_schemas(query, parsed, schema, column_source_methods):
    cte_schemas = {}
    cte_column_ranges = {}
    cte_column_metadata = {}

    for cte in parsed.find_all(exp.CTE):
        cte_name = cte.alias_or_name
        if not cte_name:
            continue

        expression = cte.this
        if not isinstance(expression, exp.Select):
            continue

        columns, column_ranges, column_metadata = collect_scope_output_columns(query, expression, schema, column_source_methods, cte_schemas, cte_column_metadata)

        if columns:
            cte_schemas[cte_name] = columns
        if column_ranges:
            cte_column_ranges[cte_name] = column_ranges
        if column_metadata:
            cte_column_metadata[cte_name] = column_metadata

    return cte_schemas, cte_column_ranges, cte_column_metadata


def resolve_table_name_from_schema(table_name, exact, short):
    normalized_name = normalize_identifier(table_name)
    if normalized_name in exact:
        return exact[normalized_name]["name"]

    short_matches = short.get(normalized_name, [])
    if len(short_matches) == 1:
        return short_matches[0]

    return None


def build_diagnostic(message, range_info, severity="error"):
    return {"message": message, "severity": severity, "range": range_info}


def describe_output_schema(dialect):
    if dialect == "duckdb":
        return {
            "column_name": "varchar",
            "column_type": "varchar",
            "null": "varchar",
            "key": "varchar",
            "default": "varchar",
            "extra": "varchar",
        }

    return {
        "column_name": "varchar",
        "column_type": "varchar",
    }


def expression_contains_subquery(expression):
    return any(isinstance(item, exp.Select) for item in expression.find_all(exp.Select))


def should_warn_unmaterialized_column(metadata):
    methods = set(metadata.get("source_methods") or [])
    return (
        bool(metadata.get("actual_schema_known"))
        and bool(methods & DEFINITION_COLUMN_METHODS)
        and not bool(methods & MATERIALIZED_COLUMN_METHODS)
    )


def build_unmaterialized_column_message(column_name, metadata):
    origin_table = metadata.get("origin_table") or "an upstream Bruin asset"
    return f"Column '{column_name}' is defined in the Bruin asset '{origin_table}', but it has not been materialized yet."


def is_path_like_table_reference(table_name, dialect):
    normalized = (table_name or "").strip()
    if not normalized:
        return False

    if normalized.startswith(("./", "../", "/", "s3://", "gs://", "http://", "https://")):
        return True

    if dialect == "duckdb" and normalized.startswith("~/"):
        return True

    return False


def collect_select_aliases(parsed):
    aliases = set()

    for select in parsed.find_all(exp.Select):
        for expression in select.expressions:
            alias = expression.alias_or_name if isinstance(expression, exp.Alias) else expression.alias
            if alias:
                aliases.add(normalize_identifier(alias))

    return aliases


def collect_order_alias_references(parsed, select_aliases):
    references = set()

    for order in parsed.find_all(exp.Order):
        for column in order.find_all(exp.Column):
            if column.table:
                continue

            column_name = normalize_identifier(column.name)
            if column_name in select_aliases:
                references.add(column_name)

    return references


def analyze_parse_context_diagnostics(query, parsed, parsed_tables, parsed_columns, schema, dialect, column_source_methods=None, output_alias_references=None, cte_schemas=None, cte_column_metadata=None):
    cte_schemas = cte_schemas or {}
    cte_column_metadata = cte_column_metadata or {}
    if not schema and not cte_schemas:
        return parsed_tables, parsed_columns, []

    combined_schema = dict(schema or {})
    combined_schema.update(cte_schemas)
    exact_schema, short_schema = build_schema_lookups(combined_schema)
    diagnostics = []
    output_alias_references = output_alias_references or set()
    aliased_relation_names = set()
    resolved_tables = []

    for table in parsed_tables:
        source_kind = table.get("source_kind", "table")
        resolved_name = table["name"] if source_kind == "cte" else resolve_table_name_from_schema(table["name"], exact_schema, short_schema)
        updated_table = dict(table)
        if resolved_name:
            updated_table["resolved_name"] = resolved_name
            if table.get("alias") and source_kind != "cte_source":
                aliased_relation_names.add(normalize_identifier(table["name"]))
                aliased_relation_names.add(normalize_identifier(resolved_name))
        else:
            updated_table["resolved_name"] = ""
            if source_kind == "table" and not is_path_like_table_reference(table["name"], dialect):
                range_info = table["parts"][-1]["range"] if table.get("parts") else None
                diagnostics.append(build_diagnostic(f"Unresolved table: {table['name']}", range_info))

        resolved_tables.append(updated_table)

    column_resolutions = {}
    resolved_columns = []

    for scope in traverse_scope(parsed):
        source_lookup = {}
        for source_name, (node, source) in scope.selected_sources.items():
            resolved_name = None
            source_columns = set()
            column_metadata = {}

            if isinstance(source, exp.Table):
                table_name = get_table_display_name(source, None, dialect)
                if is_cte_table_reference(source, cte_schemas):
                    resolved_name = source.name
                    source_columns = {normalize_identifier(column_name) for column_name in cte_schemas.get(source.name, {}).keys()}
                    column_metadata = {
                        normalize_identifier(column_name): metadata
                        for column_name, metadata in cte_column_metadata.get(source.name, {}).items()
                    }
                else:
                    resolved_name = resolve_table_name_from_schema(table_name, exact_schema, short_schema)
                    if resolved_name:
                        source_columns = exact_schema.get(normalize_identifier(resolved_name), {}).get("columns", set())
                        column_metadata = {
                            normalize_identifier(column_name): metadata
                            for column_name, metadata in schema_column_metadata_for_table(table_name, schema, column_source_methods).items()
                        }
            elif hasattr(source, "expression"):
                cte_name = getattr(node, "name", "") or source_name
                if cte_name in cte_schemas:
                    resolved_name = cte_name
                    source_columns = {normalize_identifier(column_name) for column_name in cte_schemas.get(cte_name, {}).keys()}
                    column_metadata = {
                        normalize_identifier(column_name): metadata
                        for column_name, metadata in cte_column_metadata.get(cte_name, {}).items()
                    }

            if resolved_name:
                source_lookup[normalize_identifier(source_name)] = {
                    "resolved_name": resolved_name,
                    "columns": source_columns,
                    "column_metadata": column_metadata,
                }

        if not source_lookup and scope.expression.find(exp.Describe):
            source_lookup["__describe__"] = {
                "resolved_name": "__describe__",
                "columns": {normalize_identifier(column_name) for column_name in describe_output_schema(dialect).keys()},
                "column_metadata": {},
            }

        has_confident_schema_source = len(source_lookup) > 0
        visible_column_names = {
            column_name
            for source in source_lookup.values()
            for column_name in source["columns"]
        }

        for column_expr in find_all_in_scope(scope.expression, exp.Column):
            parts = build_column_parts(query, column_expr)
            if not parts:
                continue

            column_key = id(column_expr)
            column_name = normalize_identifier(parts[-1]["name"])
            qualifier = normalize_identifier(".".join(part["name"] for part in parts[:-1]))
            resolved_table_name = None

            if qualifier:
                source = source_lookup.get(qualifier)
                if not source and qualifier not in aliased_relation_names:
                    resolved_name = resolve_table_name_from_schema(qualifier, exact_schema, short_schema)
                    if resolved_name:
                        source = {
                            "resolved_name": resolved_name,
                            "columns": exact_schema.get(normalize_identifier(resolved_name), {}).get("columns", set()),
                        }

                if not source:
                    if has_confident_schema_source:
                        diagnostics.append(build_diagnostic(f"Unresolved table or alias: {'.'.join(part['name'] for part in parts[:-1])}", parts[0]["range"]))
                else:
                    resolved_table_name = source["resolved_name"]
                    if column_name and column_name not in source["columns"]:
                        diagnostics.append(build_diagnostic(f"Unresolved column: {column_expr.sql(dialect=dialect)}", parts[-1]["range"]))
                    else:
                        metadata = source.get("column_metadata", {}).get(column_name, {})
                        if should_warn_unmaterialized_column(metadata):
                            diagnostics.append(build_diagnostic(build_unmaterialized_column_message(column_expr.sql(dialect=dialect), metadata), parts[-1]["range"], "warning"))
            else:
                candidate_sources = [
                    source
                    for source in source_lookup.values()
                    if column_name and column_name in source["columns"]
                ]

                if len(candidate_sources) == 1:
                    resolved_table_name = candidate_sources[0]["resolved_name"]
                    metadata = candidate_sources[0].get("column_metadata", {}).get(column_name, {})
                    if should_warn_unmaterialized_column(metadata):
                        diagnostics.append(build_diagnostic(build_unmaterialized_column_message(column_expr.sql(dialect=dialect), metadata), parts[-1]["range"], "warning"))
                elif column_name in output_alias_references:
                    resolved_table_name = ""
                elif len(candidate_sources) == 0 and column_name and has_confident_schema_source:
                    diagnostics.append(build_diagnostic(f"Unresolved column: {column_expr.sql(dialect=dialect)}", parts[-1]["range"]))

            column_resolutions[column_key] = resolved_table_name or ""

        for function_expr in find_all_in_scope(scope.expression, exp.Anonymous):
            function_name = normalize_identifier(getattr(function_expr, "name", "") or str(getattr(function_expr, "this", "")))
            if not function_name or function_name in visible_column_names:
                continue

            if not visible_column_names or not expression_contains_subquery(function_expr):
                continue

            range_info = build_range_from_meta(query, function_expr.meta) if hasattr(function_expr, "meta") else None
            diagnostics.append(build_diagnostic(f"Unresolved column: {function_name}", range_info))

    for column in parsed_columns:
        updated_column = dict(column)
        updated_column["resolved_table"] = column_resolutions.get(column.get("key"), "")
        updated_column.pop("key", None)
        resolved_columns.append(updated_column)

    return resolved_tables, resolved_columns, diagnostics


def get_parse_context(query, dialect, schema=None, column_source_methods=None):
    if not query or not query.strip():
        return {
            "query_kind": "",
            "is_single_select": False,
            "tables": [],
            "columns": [],
            "diagnostics": [],
            "errors": [],
        }

    try:
        parsed_statements = parse(query, dialect=dialect)
        if not parsed_statements:
            return {
                "query_kind": "",
                "is_single_select": False,
                "tables": [],
                "columns": [],
                "diagnostics": [],
                "errors": ["unable to parse query"],
            }
    except Exception as error:
        return {
            "query_kind": "",
            "is_single_select": False,
            "tables": [],
            "columns": [],
            "diagnostics": [],
            "errors": [str(error)],
        }

    first_statement = next((stmt for stmt in parsed_statements if stmt is not None), None)
    query_kind = first_statement.key if first_statement is not None else ""
    is_single_select = len(parsed_statements) == 1 and isinstance(parsed_statements[0], (exp.Select, exp.Query))

    tables = []
    columns = []
    output_alias_references = set()
    cte_schemas = {}
    cte_column_ranges = {}
    cte_column_metadata = {}
    current_database = None

    for parsed_single in parsed_statements:
        if parsed_single is None:
            continue

        if dialect == "tsql" and isinstance(parsed_single, exp.Use):
            if hasattr(parsed_single, "this") and parsed_single.this:
                current_database = parsed_single.this.name if hasattr(parsed_single.this, "name") else str(parsed_single.this)
            continue

        try:
            extracted_tables = extract_tables(parsed_single)
            parsed_cte_schemas, parsed_cte_column_ranges, parsed_cte_column_metadata = collect_cte_schemas(query, parsed_single, schema, column_source_methods or {})
            cte_schemas.update(parsed_cte_schemas)
            cte_column_ranges.update(parsed_cte_column_ranges)
            cte_column_metadata.update(parsed_cte_column_metadata)
        except Exception as error:
            return {
                "query_kind": query_kind,
                "is_single_select": is_single_select,
                "tables": [],
                "columns": [],
                "diagnostics": [],
                "errors": [str(error)],
            }

        for table in extracted_tables:
            table_name = get_table_display_name(table, current_database, dialect)
            if is_cte_table_reference(table, cte_schemas):
                source_kind = "cte"
            elif is_inside_cte_definition(table):
                source_kind = "cte_source"
            else:
                source_kind = detect_table_source_kind(table)
            tables.append(
                {
                    "name": table_name,
                    "source_kind": source_kind,
                    "columns": [
                        {"name": column_name, "type": column_type}
                        for column_name, column_type in (cte_schemas.get(table_name, {}) if source_kind == "cte" else {}).items()
                    ],
                    "column_ranges": cte_column_ranges.get(table_name, {}) if source_kind == "cte" else {},
                    "alias": table.alias,
                    "parts": build_identifier_parts(query, table.parts, "table"),
                    "alias_range": build_alias_range(query, table),
                    "scope_range": build_table_scope_range(query, table),
                    "statement_id": id(parsed_single),
                }
            )

        select_aliases = collect_select_aliases(parsed_single)
        output_alias_references.update(collect_order_alias_references(parsed_single, select_aliases))

        for scope in traverse_scope(parsed_single):
            for column in find_all_in_scope(scope.expression, exp.Column):
                parts = build_column_parts(query, column)
                if not parts:
                    continue

                qualifier = ".".join(part["name"] for part in parts[:-1])
                columns.append(
                    {
                        "key": id(column),
                        "name": ".".join(part["name"] for part in parts),
                        "qualifier": qualifier,
                        "parts": parts,
                        "statement_id": id(parsed_single),
                    }
                )

    resolved_tables, resolved_columns, diagnostics = [], [], []
    for parsed_single in parsed_statements:
        if parsed_single is None or (dialect == "tsql" and isinstance(parsed_single, exp.Use)):
            continue

        statement_tables = [table for table in tables if table.get("statement_id") == id(parsed_single)]
        statement_columns = [column for column in columns if column.get("statement_id") == id(parsed_single)]
        for item in statement_tables:
            item.pop("statement_id", None)
        for item in statement_columns:
            item.pop("statement_id", None)
        analyzed_tables, analyzed_columns, analyzed_diagnostics = analyze_parse_context_diagnostics(query, parsed_single, statement_tables, statement_columns, schema, dialect, column_source_methods or {}, output_alias_references, cte_schemas, cte_column_metadata)
        resolved_tables.extend(analyzed_tables)
        resolved_columns.extend(analyzed_columns)
        diagnostics.extend(analyzed_diagnostics)
    return {
        "query_kind": query_kind,
        "is_single_select": is_single_select,
        "tables": resolved_tables,
        "columns": resolved_columns,
        "diagnostics": diagnostics,
        "errors": [],
    }


def main():
    try:
        request = json.loads(sys.stdin.readline())
        response = get_parse_context(
            request.get("query", ""),
            request.get("dialect", ""),
            request.get("schema") or {},
            request.get("column_source_methods") or {},
        )
    except Exception as error:
        response = {"error": str(error)}

    sys.stdout.write(json.dumps(response) + "\n")
    sys.stdout.flush()


if __name__ == "__main__":
    main()
