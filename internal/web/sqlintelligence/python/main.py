import json
import sys

from sqlglot import exp, parse
from sqlglot.optimizer.scope import find_all_in_scope


def extract_tables(parsed):
    if parsed is None:
        return []

    def get_cte_names(parsed_stmt):
        cte_names = set()

        if isinstance(parsed_stmt, exp.Create):
            if parsed_stmt.expression:
                for cte in parsed_stmt.expression.find_all(exp.CTE):
                    cte_names.add(cte.alias_or_name)
        else:
            for cte in parsed_stmt.find_all(exp.CTE):
                cte_names.add(cte.alias_or_name)

        return cte_names

    def extract_table_references(stmt, cte_names):
        table_refs = []

        for table in stmt.find_all(exp.Table):
            actual_table_name = table.name
            is_unaliased_cte_definition_reference = (
                actual_table_name in cte_names
                and not table.db
                and not table.catalog
                and not table.alias
            )
            if is_unaliased_cte_definition_reference:
                continue

            table_refs.append(table)

        return table_refs

    cte_names = get_cte_names(parsed)
    return extract_table_references(parsed, cte_names)


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


def collect_cte_schemas(query, parsed):
    cte_schemas = {}
    cte_column_ranges = {}

    for cte in parsed.find_all(exp.CTE):
        cte_name = cte.alias_or_name
        if not cte_name:
            continue

        columns = {}
        column_ranges = {}
        expression = cte.this
        if not isinstance(expression, exp.Select):
            continue

        for select_expression in expression.expressions:
            name = select_expression.alias_or_name
            if name:
                columns[name] = ""
                alias_range = build_expression_alias_range(query, select_expression)
                if alias_range:
                    column_ranges[name] = alias_range

        if columns:
            cte_schemas[cte_name] = columns
        if column_ranges:
            cte_column_ranges[cte_name] = column_ranges

    return cte_schemas, cte_column_ranges


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


def analyze_parse_context_diagnostics(parsed_tables, parsed_columns, schema, dialect, output_alias_references=None, cte_schemas=None):
    cte_schemas = cte_schemas or {}
    if not schema and not cte_schemas:
        return parsed_tables, parsed_columns, []

    combined_schema = dict(schema or {})
    combined_schema.update(cte_schemas)
    exact_schema, short_schema = build_schema_lookups(combined_schema)
    diagnostics = []
    output_alias_references = output_alias_references or set()
    alias_lookup = {}
    aliased_relation_names = set()
    resolved_tables = []

    for table in parsed_tables:
        source_kind = table.get("source_kind", "table")
        resolved_name = table["name"] if source_kind == "cte" else resolve_table_name_from_schema(table["name"], exact_schema, short_schema)
        updated_table = dict(table)
        if resolved_name:
            updated_table["resolved_name"] = resolved_name
            if table.get("alias") and source_kind != "cte_source":
                alias_lookup[normalize_identifier(table["alias"])] = resolved_name
                aliased_relation_names.add(normalize_identifier(table["name"]))
                aliased_relation_names.add(normalize_identifier(resolved_name))
        else:
            updated_table["resolved_name"] = ""
            if source_kind == "table" and not is_path_like_table_reference(table["name"], dialect):
                range_info = table["parts"][-1]["range"] if table.get("parts") else None
                diagnostics.append(build_diagnostic(f"Unresolved table: {table['name']}", range_info))

        resolved_tables.append(updated_table)

    resolved_columns = []
    resolved_table_names = [
        table.get("resolved_name", "")
        for table in resolved_tables
        if table.get("resolved_name") and table.get("source_kind", "table") in ("table", "cte")
    ]
    has_confident_schema_source = len(resolved_table_names) > 0

    for column in parsed_columns:
        updated_column = dict(column)
        parts = column.get("parts", [])
        column_name = normalize_identifier(parts[-1]["name"]) if parts else ""
        qualifier = normalize_identifier(column.get("qualifier", ""))
        resolved_table_name = None

        if qualifier:
            resolved_table_name = alias_lookup.get(qualifier)
            if not resolved_table_name and qualifier not in aliased_relation_names:
                resolved_table_name = resolve_table_name_from_schema(qualifier, exact_schema, short_schema)

            if not resolved_table_name:
                range_info = parts[0]["range"] if parts else None
                if has_confident_schema_source:
                    diagnostics.append(build_diagnostic(f"Unresolved table or alias: {column.get('qualifier', '')}", range_info))
            else:
                schema_columns = exact_schema.get(normalize_identifier(resolved_table_name), {}).get("columns", set())
                if column_name and column_name not in schema_columns:
                    diagnostics.append(build_diagnostic(f"Unresolved column: {column.get('name', '')}", parts[-1]["range"] if parts else None))
        else:
            candidate_tables = []
            for table_name in resolved_table_names:
                schema_columns = exact_schema.get(normalize_identifier(table_name), {}).get("columns", set())
                if column_name and column_name in schema_columns:
                    candidate_tables.append(table_name)

            if len(candidate_tables) == 1:
                resolved_table_name = candidate_tables[0]
            elif column_name in output_alias_references:
                resolved_table_name = ""
            elif len(candidate_tables) == 0 and column_name and has_confident_schema_source:
                diagnostics.append(build_diagnostic(f"Unresolved column: {column.get('name', '')}", parts[-1]["range"] if parts else None))

        updated_column["resolved_table"] = resolved_table_name or ""
        resolved_columns.append(updated_column)

    return resolved_tables, resolved_columns, diagnostics


def get_parse_context(query, dialect, schema=None):
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
            parsed_cte_schemas, parsed_cte_column_ranges = collect_cte_schemas(query, parsed_single)
            cte_schemas.update(parsed_cte_schemas)
            cte_column_ranges.update(parsed_cte_column_ranges)
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
                }
            )

        select_aliases = collect_select_aliases(parsed_single)
        output_alias_references.update(collect_order_alias_references(parsed_single, select_aliases))

        for column in find_all_in_scope(parsed_single, exp.Column):
            parts = build_column_parts(query, column)
            if not parts:
                continue

            qualifier = ".".join(part["name"] for part in parts[:-1])
            columns.append(
                {
                    "name": ".".join(part["name"] for part in parts),
                    "qualifier": qualifier,
                    "parts": parts,
                }
            )

    resolved_tables, resolved_columns, diagnostics = analyze_parse_context_diagnostics(tables, columns, schema, dialect, output_alias_references, cte_schemas)
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
        )
    except Exception as error:
        response = {"error": str(error)}

    sys.stdout.write(json.dumps(response) + "\n")
    sys.stdout.flush()


if __name__ == "__main__":
    main()
