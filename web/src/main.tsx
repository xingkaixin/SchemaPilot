import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toasty, TooltipProvider } from "@cloudflare/kumo";
import { App } from "./components/App";
import { toasts } from "./toasts";
import "./styles.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: true } },
});

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
