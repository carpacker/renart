import { atom } from "jotai";

export type EditorDraftState = Record<string, string>;

export const editorDraftAtom = atom<EditorDraftState>({});

export type AssetEditorTab =
  | "configuration"
  | "checks"
  | "visualization"
  | "dependencies";

export const assetEditorTabAtom = atom<AssetEditorTab>("configuration");
