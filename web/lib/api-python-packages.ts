import { fetchJSON } from "@/lib/api-core";

export type PythonPackage = {
  name: string;
  summary: string;
  version: string;
};

type PackageSearchResponse = {
  status: "ok" | "error";
  packages: PythonPackage[];
};

/** Probable PyPI packages for a Python import name (with summaries). */
export async function searchPythonPackages(importName: string): Promise<PythonPackage[]> {
  const response = await fetchJSON<PackageSearchResponse>(
    `/api/python/packages?import=${encodeURIComponent(importName)}`
  );
  return response.packages ?? [];
}
