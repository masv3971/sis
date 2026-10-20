import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./App";
import { setToken } from "./api";
import { completeSignIn } from "./oidc";
import "./styles.css";

// Finish a sign-in the issuer redirected back from before first render, so
// App starts signed in. A failure is shown on the sign-in page.
void completeSignIn()
  .then((token) => {
    if (token) setToken(token);
    return "";
  })
  .catch((e: unknown) => (e as Error).message)
  .then((signInError) => {
    ReactDOM.createRoot(document.getElementById("root")!).render(
      <React.StrictMode>
        <App signInError={signInError} />
      </React.StrictMode>,
    );
  });
