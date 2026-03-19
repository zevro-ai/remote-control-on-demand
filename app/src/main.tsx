import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import { WebSocketProvider } from "./hooks/WebSocketContext";
import App from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <WebSocketProvider>
      <App />
    </WebSocketProvider>
  </StrictMode>
);
