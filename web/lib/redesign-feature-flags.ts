export type RedesignFeatureFlag = "aiChat" | "notifications" | "profileMenu";

// Feature flags for the redesign UI. Flip a flag to true to re-enable the
// corresponding surface — these gate features that are intentionally hidden
// until they actually work end-to-end, rather than being deleted from the tree.
// Typed as booleans (not literal `false`) so callers aren't narrowed into
// dead-branch warnings while a flag is off.
export const redesignFeatureFlags: Record<RedesignFeatureFlag, boolean> = {
  // AI builder chat (the Sparkles button + side sheet in the top bar).
  aiChat: false,
  // Notifications bell in the top bar.
  notifications: false,
  // Account / profile menu in the top bar.
  profileMenu: false,
};
