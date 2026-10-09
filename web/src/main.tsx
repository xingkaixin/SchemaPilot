import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { Toasty, TooltipProvider } from "@cloudflare/kumo";
import { App } from "./components/App";
import { queryClient } from "./queryClient";
import { toasts } from "./toasts";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <Toasty toastManager={toasts}>
        <TooltipProvider>
          <App />
        </TooltipProvider>
      </Toasty>
    </QueryClientProvider>
  </StrictMode>,
);
