import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { TooltipProvider } from "@/components/ui/tooltip";
import { loadMonacoEditorModule } from "@/lib/load-monaco-editor";
import { AppRouter } from "./router";
import "./globals.css";

void loadMonacoEditorModule();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <TooltipProvider>
      <AppRouter />
    </TooltipProvider>
  </StrictMode>
);
