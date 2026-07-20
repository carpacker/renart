import { atom } from "jotai";

export type EditorDraftState = Record<string, string>;

export const editorDraftAtom = atom<EditorDraftState>({});

export type EditorProgrammaticContentState = Record<string, { content: string; revision: number }>;

export const editorProgrammaticContentAtom = atom<EditorProgrammaticContentState>({});

export type AssetEditorTab = "configuration" | "checks" | "visualization" | "dependencies";

export const assetEditorTabAtom = atom<AssetEditorTab>("configuration");

// Transient editor-to-canvas affordance. This is intentionally not persisted:
// it exists only while the pointer rests on a SQL relation reference.
export const sqlHoveredAssetAtom = atom<string | null>(null);
