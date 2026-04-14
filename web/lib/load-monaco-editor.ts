let monacoEditorModulePromise: Promise<typeof import("@monaco-editor/react")> | null = null;

export function loadMonacoEditorModule() {
  if (!monacoEditorModulePromise) {
    monacoEditorModulePromise = import("@monaco-editor/react").then((module) => {
      module.loader.config({
        paths: {
          vs: "/monaco/vs",
        },
      });

      return module;
    });
  }

  return monacoEditorModulePromise;
}
