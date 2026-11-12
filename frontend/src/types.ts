export type Provider = "claude" | "codex" | "grok" | "antigravity";

export interface Tier {
  id: string;
  label: string;
  rank: number;
}

export interface Account {
  id: string;
  name: string;
  provider: Provider;
  created: string;
  credentialFile: string;
  activatedAt?: string;
  renewalAt?: string;
  /** True while renewalAt is an estimate adopted from a poll. */
  renewalEstimated?: boolean;
  tierId: string;
  ladder: boolean;
  changeCommand?: string;
  autoReset: boolean;
  home: string;
  agentHome: string;
  agentBin?: string;
  usageThreshold?: number;
}

export interface WindowMeter {
  label: string;
  util: number | null;
  resetsAt?: string;
}

export interface Credits {
  enabled: boolean;
  used?: number;
  limit?: number;
  currency?: string;
  balance?: number;
  unlimited: boolean;
  resetCount?: number;
  resetApplies?: number;
  /** Each banked reset, when the provider itemises them. Soonest to expire first. */
  resets?: ResetCredit[];
}

export interface Reading {
  /** True when renewalAt was worked out from the subscription start, not stated. */
  renewalEstimated?: boolean;
  ok: boolean;
  error?: string;
  stale: boolean;
  polledAt: string;
  /** Who the credential copy is signed in as, when the file says. */
  account?: string;
  plan?: string;
  tierId?: string;
  windows: WindowMeter[];
  credits: Credits;
}

export type Phase = "off" | "unarmed" | "climbing" | "waiting" | "acting";

export interface LadderState {
  phase: Phase;
  tierId: string;
  note: string;
  lastStep?: string;
  lastError?: string;
  cycles: number;
}

export interface Step {
  from: string;
  to: string;
  reason: string;
  command?: string;
  /** A step that changes no plan: it uses a banked reset on the tier in from and to. */
  reset?: boolean;
  creditId?: string;
}

export interface AccountView {
  account: Account;
  reading?: Reading;
  ladder: LadderState;
  next?: Step;
  tiers: Tier[];
  /** The last plan changes, newest first. Absent when there are none. */
  history?: HistoryEntry[];
}

/** One attempted plan change. error is absent when the change command exited 0. */
export interface HistoryEntry {
  at: string;
  from: string;
  to: string;
  error?: string;
  /** True for a banked reset used on the tier in from and to. */
  reset?: boolean;
}

export interface UpdateStatus {
  current: string;
  latest?: string;
  available: boolean;
  ahead: boolean;
  note?: string;
  checkedAt?: string;
}

export interface Overview {
  version: string;
  dataDir: string;
  accounts: AccountView[];
  polledAt?: string;
  update: UpdateStatus;
  sandboxOk: boolean;
  sandboxNote?: string;
  settings: Settings;
  /** Account directories that could not be loaded and are missing from accounts. */
  problems: string[];
  /** Every account's proposed next steps, soonest first. */
  advice: Advice[];
}

export interface ProbeResult {
  accountId: string;
  ok: boolean;
  exit: number;
  output: string;
  sandboxed: boolean;
  note?: string;
}

export interface ProviderInfo {
  id: Provider;
  title: string;
  tiers: Tier[];
  /** The provider CLI's default login file. Empty when the home directory is unknown. */
  credentialPath?: string;
  /** subman can sign in to this provider itself, with no credential file. */
  login: boolean;
}

export interface ImportRequest {
  name: string;
  provider: Provider;
  credentialPath: string;
  tierId: string;
  activatedAt: string;
  renewalAt: string;
}

/** The window's preferences, stored in settings.json in the data directory. */
export interface Settings {
  /** 0 is never. One of model.AutoPollChoices. */
  autoPollMinutes: number;
  notify: boolean;
  foldedProviders: Provider[];
}

/** A browser sign-in that has started. userCode is set for a device sign-in: the code the page shows. */
export interface LoginStart {
  url: string;
  userCode?: string;
}

/** One banked rate-limit reset. expiresAt is absent for one that does not expire. */
export interface ResetCredit {
  id: string;
  type: string;
  title?: string;
  status?: string;
  grantedAt?: string;
  expiresAt?: string;
}

/** One proposed step toward spending every paid window in full, and when to take it. */
export interface Advice {
  accountId: string;
  account: string;
  at: string;
  kind: "step" | "reset" | "drop" | "spend" | "expiring" | "renewal" | "login";
  text: string;
}
