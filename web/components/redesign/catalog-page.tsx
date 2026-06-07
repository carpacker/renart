import { Filter, RotateCw, Search, Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";

import { assets, edges, getAsset } from "./redesign-data";
import { AssetNode, PageHeader, RedesignPage, RedesignPanel } from "./redesign-primitives";

export function RedesignCatalogPage() {
  const groups = assets.reduce<Record<string, typeof assets>>((acc, asset) => {
    acc[asset.group] = [...(acc[asset.group] ?? []), asset];
    return acc;
  }, {});

  return (
    <RedesignPage>
      <PageHeader
        title="Catalog"
        subtitle="Explore asset lineage across data_platform"
        actions={<Button variant="outline" size="sm"><RotateCw className="size-3.5" />Reload</Button>}
      />
      <div className="flex items-center gap-2 px-3 pb-2">
        <Button variant="outline" size="sm"><Filter className="size-3.5" />Filter</Button>
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-8" placeholder="Type an asset subset...  (ex: ++revenue_daily)" />
        </div>
        <Button size="sm"><Sparkles className="size-3.5" />Materialize all</Button>
      </div>
      <div className="min-h-0 flex-1 px-3 pb-3">
        <RedesignPanel className="h-full">
          <ScrollArea className="h-full" viewportClassName="h-full">
            <div
              className="relative h-[540px] w-[1220px]"
              style={{ backgroundImage: "radial-gradient(rgba(0,0,0,0.08) 1px, transparent 1px)", backgroundSize: "22px 22px" }}
            >
              {Object.entries(groups).map(([name, groupAssets]) => {
                const items = groupAssets ?? [];
                const minX = Math.min(...items.map((asset) => asset.x)) - 16;
                const minY = Math.min(...items.map((asset) => asset.y)) - 42;
                const maxX = Math.max(...items.map((asset) => asset.x)) + 248;
                const maxY = Math.max(...items.map((asset) => asset.y)) + 112;
                return (
                  <div key={name} className="absolute rounded-2xl border bg-background/50" style={{ left: minX, top: minY, width: maxX - minX, height: maxY - minY }}>
                    <div className="absolute left-3 top-2.5 flex items-center gap-2">
                      <span className="font-mono text-xs font-semibold">{name}</span>
                      <span className="rounded-full bg-primary/10 px-1.5 text-[10px] text-primary">{items.length}</span>
                    </div>
                  </div>
                );
              })}
              <svg className="absolute inset-0 pointer-events-none" width="1220" height="540">
                {edges.map(([from, to]) => {
                  const source = getAsset(from);
                  const target = getAsset(to);
                  return <path key={`${from}-${to}`} d={`M${source.x + 232},${source.y + 48} C${source.x + 280},${source.y + 48} ${target.x - 48},${target.y + 48} ${target.x},${target.y + 48}`} stroke="#a1a1aa" strokeWidth="1.5" fill="none" />;
                })}
              </svg>
              {assets.map((asset) => (
                <div key={asset.id} className="absolute" style={{ left: asset.x, top: asset.y }}>
                  <AssetNode asset={asset} />
                </div>
              ))}
            </div>
          </ScrollArea>
        </RedesignPanel>
      </div>
    </RedesignPage>
  );
}
