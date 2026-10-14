import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./App";
import { consumeTokenFromHash } from "./api";
import "./styles.css";

consumeTokenFromHash(); // pick up an OIDC callback token before first render

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
