// The window calls the Go app through Wails. In a plain browser (vite dev
// with no shell) every call fails the same way, and the page says so.

import type { Account, AccountView, ImportRequest, LoginStart, Overview, ProbeResult, ProviderInfo, Settings, UpdateStatus } from "./types";

interface Bridge {
  Overview(): Promise<Overview>;
  Providers(): Promise<ProviderInfo[]>;
  Import(req: ImportRequest): Promise<AccountView>;
  UpdateAccount(acc: Account): Promise<AccountView>;
  Delete(id: string): Promise<void>;
  Poll(): Promise<Overview>;
  PollOne(id: string): Promise<AccountView>;
  Renew(id: string): Promise<AccountView>;
  RunLadder(): Promise<Overview>;
  TestAccount(id: string): Promise<ProbeResult>;
  CheckUpdate(): Promise<UpdateStatus>;
  SaveSettings(s: Settings): Promise<void>;
  BeginLogin(req: ImportRequest): Promise<LoginStart>;
  BeginRelogin(id: string): Promise<LoginStart>;
  CancelLogin(): Promise<void>;
  CompleteLogin(req: ImportRequest): Promise<AccountView>;
  CompleteRelogin(id: string): Promise<AccountView>;
  PickCredential(startAt: string): Promise<string>;
  ReplaceCredential(id: string, path: string): Promise<AccountView>;
}

// Wails names the binding after the Go package and the type:
// window.go.app.App. The runtime installs it after the page loads, so a
// caller that looks once at startup misses it.
function bridge(): Bridge | null {
  const w = window as unknown as { go?: { app?: { App?: Bridge } } };
  return w.go?.app?.App ?? null;
}

export function whenBridge(timeoutMs = 4000): Promise<boolean> {
  if (bridge()) {
    return Promise.resolve(true);
  }
  return new Promise((resolve) => {
    const started = Date.now();
    const tick = () => {
      if (bridge()) {
        resolve(true);
        return;
      }
      if (Date.now() - started > timeoutMs) {
        resolve(false);
        return;
      }
      setTimeout(tick, 50);
    };
    tick();
  });
}

export function hasBridge(): boolean {
  return bridge() !== null;
}

async function call<T>(fn: (b: Bridge) => Promise<T>): Promise<T> {
  const b = bridge();
  if (!b) {
    throw new Error("not running inside the subman window");
  }
  return fn(b);
}

export const api = {
  overview: () => call((b) => b.Overview()),
  providers: () => call((b) => b.Providers()),
  importAccount: (req: ImportRequest) => call((b) => b.Import(req)),
  updateAccount: (acc: Account) => call((b) => b.UpdateAccount(acc)),
  deleteAccount: (id: string) => call((b) => b.Delete(id)),
  poll: () => call((b) => b.Poll()),
  pollOne: (id: string) => call((b) => b.PollOne(id)),
  renew: (id: string) => call((b) => b.Renew(id)),
  runLadder: () => call((b) => b.RunLadder()),
  testAccount: (id: string) => call((b) => b.TestAccount(id)),
  checkUpdate: () => call((b) => b.CheckUpdate()),
  saveSettings: (s: Settings) => call((b) => b.SaveSettings(s)),
  beginLogin: (req: ImportRequest) => call((b) => b.BeginLogin(req)),
  beginRelogin: (id: string) => call((b) => b.BeginRelogin(id)),
  cancelLogin: () => call((b) => b.CancelLogin()),
  completeLogin: (req: ImportRequest) => call((b) => b.CompleteLogin(req)),
  completeRelogin: (id: string) => call((b) => b.CompleteRelogin(id)),
  pickCredential: (startAt: string) => call((b) => b.PickCredential(startAt)),
  replaceCredential: (id: string, path: string) => call((b) => b.ReplaceCredential(id, path)),
};

/**
 * Calls cb with an account id each time Poll finishes that account. Returns
 * the unsubscribe function. Outside the subman window there is no event
 * runtime, so nothing is subscribed.
 */
export function onPolled(cb: (id: string) => void): () => void {
  const w = window as unknown as { runtime?: { EventsOn?: (name: string, fn: (id: string) => void) => () => void } };
  return w.runtime?.EventsOn?.("polled", cb) ?? (() => {});
}

/** Opens url in the system browser through the Wails runtime. Outside the subman window it does nothing. */
export function openExternal(url: string): void {
  const w = window as unknown as { runtime?: { BrowserOpenURL?: (url: string) => void } };
  w.runtime?.BrowserOpenURL?.(url);
}
