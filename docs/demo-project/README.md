# Acme Shop — docs demo pipeline

A small, self-contained e-commerce analytics pipeline used for the Renart
documentation and its screenshots. It runs entirely on local DuckDB (no external
connections) so anyone can open it and follow along.

Assets are grouped into folders under `acme_shop/assets/` — `raw/`, `staging/`, and
`marts/`. The folder is the layer: an asset in `assets/staging/` is named
`staging.<name>` and materialises into a `staging` schema. No `raw_`/`stg_` name
prefixes.

Shape: four seeds (`raw`) → four staging views (`staging`) → three marts (`marts`).

```
raw.customers   ─▶ staging.customers   ─┐
raw.products    ─▶ staging.products    ─┤
raw.orders      ─▶ staging.orders      ─┼▶ marts.order_items_enriched ─┬▶ marts.customer_revenue
raw.order_items ─▶ staging.order_items ─┘                             └▶ marts.daily_revenue
```

Open it in Renart with the workspace root set to this folder.
