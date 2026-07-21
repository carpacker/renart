export const LOCAL_LOAD_CONNECTION = "local";

export function isLocalLoadConnection(name: string | undefined) {
  return (name ?? "").trim().toLowerCase() === LOCAL_LOAD_CONNECTION;
}

export function loadTargetNeedsDestinationObject(category: string | undefined) {
  return category === "storage" || category === "file";
}
