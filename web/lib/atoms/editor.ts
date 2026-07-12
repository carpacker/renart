import { atom } from "jotai";

export type EditorDraftState = Record<string, string>;

export const editorDraftAtom = atom<EditorDraftState>({});

export type EditorProgrammaticContentState = Record<string, { content: string; revision: number }>;

export const editorProgrammaticContentAtom = atom<EditorProgrammaticContentState>({});

export type AssetEditorTab = "configuration" | "checks" | "visualization" | "dependencies";

export const assetEditorTabAtom = atom<AssetEditorTab>("configuration");
