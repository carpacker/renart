import { atom } from "jotai";

import { selectedAssetDataAtom } from "./selection";

export type EditorDraftState = Record<string, string>;

export const editorDraftAtom = atom<EditorDraftState>({});

export const editorValueAtom = atom<string>((get) => {
  const asset = get(selectedAssetDataAtom);
  const editorDraft = get(editorDraftAtom);

  if (asset && editorDraft[asset.id] !== undefined) {
    return editorDraft[asset.id];
  }

  return asset?.content ?? "";
});

export type AssetEditorTab =
  | "configuration"
  | "checks"
  | "visualization"
  | "dependencies";

export const assetEditorTabAtom = atom<AssetEditorTab>("configuration");
