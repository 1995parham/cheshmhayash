import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App.tsx";
import "./styles.css";

function render() {
  const root = document.getElementById("root");
  if (!root) throw new Error("missing #root element");

  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}

// `vite build --mode demo` swaps the backend for a recorded snapshot
// (src/demo/shim.ts); every other build drops this branch.
if (import.meta.env.MODE === "demo") {
  void import("./demo/shim").then(render);
} else {
  render();
}
