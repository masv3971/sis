import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./App";
import { completeOIDCLogin } from "./api";
import "./styles.css";

// Pick up a pending OIDC login before first render, so App starts signed in.
// completeOIDCLogin never rejects.
void completeOIDCLogin().then(() => {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
});
