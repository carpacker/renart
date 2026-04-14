import { atom } from "jotai";

import { getSQLDatabases, getSQLTableColumns, getSQLTables } from "@/lib/api";

type SQLTableDiscoveryEntry = {
  name: string;
  short_name: string;
};

type SQLDiscoveryCacheState = {
  databasesByScope: Record<string, string[]>;
  tablesByScope: Record<string, SQLTableDiscoveryEntry[]>;
  columnsByScope: Record<string, Array<{ name: string; type?: string }>>;
};

const initialState: SQLDiscoveryCacheState = {
  databasesByScope: {},
  tablesByScope: {},
  columnsByScope: {},
};

export const sqlDiscoveryCacheAtom = atom<SQLDiscoveryCacheState>(initialState);

const pendingTableDiscoveryRequests = new Map<string, Promise<SQLTableDiscoveryEntry[]>>();
const pendingColumnDiscoveryRequests = new Map<string, Promise<Array<{ name: string; type?: string }>>>();

function discoveryScopeKey(connection: string, environment?: string) {
  return `${connection}::${environment ?? ""}`;
}

export const sqlDiscoveryTablesAtom = atom(
  (get) => get(sqlDiscoveryCacheAtom).tablesByScope,
  async (get, set, options: { connection: string; environment?: string }) => {
    const scopeKey = discoveryScopeKey(options.connection, options.environment);
    const cached = get(sqlDiscoveryCacheAtom).tablesByScope[scopeKey];
    if (cached) {
      return cached;
    }

    const pending = pendingTableDiscoveryRequests.get(scopeKey);
    if (pending) {
      return pending;
    }

    const request = (async () => {
      const databasesResponse = await getSQLDatabases({
        connection: options.connection,
        environment: options.environment,
      });
      const databaseNames = databasesResponse.databases ?? [];
      const tableResponses = await Promise.all(
        databaseNames.map(async (databaseName) => {
          try {
            return await getSQLTables({
              connection: options.connection,
              database: databaseName,
              environment: options.environment,
            });
          } catch {
            return null;
          }
        }),
      );

      const tables = tableResponses.flatMap((response) => response?.tables ?? []);

      set(sqlDiscoveryCacheAtom, (previous) => ({
        databasesByScope: {
          ...previous.databasesByScope,
          [scopeKey]: databaseNames,
        },
        tablesByScope: {
          ...previous.tablesByScope,
          [scopeKey]: tables,
        },
        columnsByScope: previous.columnsByScope,
      }));

      return tables;
    })();

    pendingTableDiscoveryRequests.set(scopeKey, request);
    try {
      return await request;
    } finally {
      pendingTableDiscoveryRequests.delete(scopeKey);
    }
  },
);

function columnScopeKey(connection: string, table: string, environment?: string) {
	return `${connection}::${environment ?? ""}::${table.toLowerCase()}`;
}

export const sqlDiscoveryColumnsAtom = atom(
	(get) => get(sqlDiscoveryCacheAtom).columnsByScope,
	async (get, set, options: { connection: string; table: string; environment?: string }) => {
		const scopeKey = columnScopeKey(options.connection, options.table, options.environment);
		const cached = get(sqlDiscoveryCacheAtom).columnsByScope[scopeKey];
		if (cached) {
			return cached;
		}

		const pending = pendingColumnDiscoveryRequests.get(scopeKey);
		if (pending) {
			return pending;
		}

		const request = (async () => {
			const response = await getSQLTableColumns({
				connection: options.connection,
				table: options.table,
				environment: options.environment,
			});
			const columns = response.columns ?? [];

			set(sqlDiscoveryCacheAtom, (previous) => ({
				databasesByScope: previous.databasesByScope,
				tablesByScope: previous.tablesByScope,
				columnsByScope: {
					...previous.columnsByScope,
					[scopeKey]: columns,
				},
			}));

			return columns;
		})();

		pendingColumnDiscoveryRequests.set(scopeKey, request);
		try {
			return await request;
		} finally {
			pendingColumnDiscoveryRequests.delete(scopeKey);
		}
	},
);
