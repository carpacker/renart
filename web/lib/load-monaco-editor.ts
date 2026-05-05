let monacoEditorModulePromise: Promise<typeof import("@monaco-editor/react")> | null = null;
let monacoWarmupPromise: Promise<unknown> | null = null;

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

export function warmMonacoEditorRuntime() {
  if (!monacoWarmupPromise) {
    monacoWarmupPromise = loadMonacoEditorModule()
      .then((module) => module.loader.init())
      .catch((error) => {
        monacoWarmupPromise = null;
        throw error;
      });
  }

  return monacoWarmupPromise;
}
