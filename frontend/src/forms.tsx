import type { ComponentChildren } from "preact";
import { useEffect, useId, useRef, useState } from "preact/hooks";
import { FolderOpen } from "lucide-preact";
import { api, openExternal } from "./api";
import { day, fromDay } from "./format";
import type { Account, AccountView, ImportRequest, LoginStart, ProviderInfo } from "./types";
import { messageOf, title } from "./shared";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";

export function SettingsSheet({
  view, busy, errorLine, defaultCredential, login, canSignIn, onClose, onSave, onReplace, onSignIn, onCancelSignIn, onDelete,
}: {
  view: AccountView;
  busy: string;
  errorLine: ComponentChildren;
  /** Where the provider CLI keeps its login, offered as the file to copy from. */
  defaultCredential: string;
  onClose: () => void;
  onSave: (acc: Account) => void;
  onReplace: (path: string) => void;
  /** The sign-in in progress, when there is one. */
  login: LoginStart | null;
  /** subman can sign in to this account's provider itself. */
  canSignIn: boolean;
  onSignIn: () => void;
  onCancelSignIn: () => void;
  onDelete: () => void;
}) {
  const [form, setForm] = useState(view.account);
  const [credential, setCredential] = useState(defaultCredential);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  // The account as it was when the sheet opened; an edit is anything that differs from it.
  const opened = useRef(view.account);
  const openedThreshold = useRef(String(view.threshold));
  // Dates are edited as text and turned into instants on save; an empty field clears the date.
  const [activated, setActivated] = useState(day(view.account.activatedAt));
  const [renewal, setRenewal] = useState(day(view.account.renewalAt));
  // The threshold is edited as text too, so the field can be emptied while typing.
  const [threshold, setThreshold] = useState(String(view.threshold));
  const [formError, setFormError] = useState("");
  // The field the error belongs to, so it is announced with that field and
  // focus lands on it (WCAG 3.3.1).
  const [errorField, setErrorField] = useState("");
  const errorId = useId();
  const ladderId = useId();
  const resetId = useId();
  const formRef = useRef<HTMLFormElement>(null);
  useEffect(() => {
    if (!formError || !errorField) return;
    formRef.current?.querySelector<HTMLElement>(`[name="${errorField}"]`)?.focus();
  }, [formError, errorField]);
  // "" and a missing field are the same thing here: typing into an optional
  // field and emptying it again is not an edit.
  const settled = (acc: Account) => JSON.stringify(acc, (_key, value: unknown) => (value === "" ? undefined : value));
  const dirty =
    settled(form) !== settled(opened.current) ||
    activated !== day(opened.current.activatedAt) ||
    renewal !== day(opened.current.renewalAt) ||
    threshold !== openedThreshold.current;

  return (
    <Sheet
      open
      onOpenChange={(v) => {
        if (v) return;
        if (dirty) setConfirmDiscard(true);
        else onClose();
      }}
    >
      <SheetContent
        className="gap-0 sm:max-w-form"
        // Coming back from the browser, the first click lands outside the sheet. It must not end the sign-in.
        onInteractOutside={(e) => { if (login) e.preventDefault(); }}
      >
        <SheetHeader>
          <SheetTitle>Edit {view.account.name}</SheetTitle>
          <SheetDescription className="break-all font-mono text-micro">{view.account.agentHome}</SheetDescription>
        </SheetHeader>
        <form
          ref={formRef}
          className="flex min-h-0 flex-1 flex-col"
          aria-busy={busy === "save" || busy === "replace" || undefined}
          onSubmit={(e) => {
            e.preventDefault();
            const dates = { activatedAt: fromDay(activated), renewalAt: fromDay(renewal) };
            if (activated && !dates.activatedAt) {
              setErrorField("activatedAt");
              setFormError("Activated needs a real date as YYYY-MM-DD, or nothing.");
              return;
            }
            if (renewal && !dates.renewalAt) {
              setErrorField("renewalAt");
              setFormError("Renewal needs a real date as YYYY-MM-DD, or nothing.");
              return;
            }
            const usageThreshold = Number(threshold);
            if (!(usageThreshold >= 1 && usageThreshold <= 100)) {
              setErrorField("usageThreshold");
              setFormError("The spend threshold is a number from 1 to 100.");
              return;
            }
            setFormError("");
            setErrorField("");
            onSave({ ...form, ...dates, usageThreshold });
          }}
        >
          <div className="flex flex-1 flex-col gap-4 overflow-auto px-4 pb-4">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Name"><Input required value={form.name} onInput={(e) => setForm({ ...form, name: e.currentTarget.value })} /></Field>
              <Field label="Tier">
                <NativeSelect value={form.tierId} onChange={(e) => setForm({ ...form, tierId: e.currentTarget.value })}>
                  <NativeSelectOption value="">not reported</NativeSelectOption>
                  {view.tiers.map((t) => <NativeSelectOption key={t.id} value={t.id}>{t.label}</NativeSelectOption>)}
                </NativeSelect>
              </Field>
              <Field label="Activated"><DayInput name="activatedAt" invalid={errorField === "activatedAt"} describedBy={errorField === "activatedAt" ? errorId : undefined} value={activated} onChange={setActivated} /></Field>
              <Field label="Renewal"><DayInput name="renewalAt" invalid={errorField === "renewalAt"} describedBy={errorField === "renewalAt" ? errorId : undefined} value={renewal} onChange={setRenewal} /></Field>
              <Field label="Spend threshold, %"><Input name="usageThreshold" inputMode="numeric" aria-invalid={errorField === "usageThreshold" || undefined} aria-describedby={errorField === "usageThreshold" ? errorId : undefined} value={threshold} onInput={(e) => setThreshold(e.currentTarget.value)} /></Field>
              <Field label="Agent binary"><Input placeholder="the one on PATH" value={form.agentBin || ""} onInput={(e) => setForm({ ...form, agentBin: e.currentTarget.value })} /></Field>
            </div>
            <p className="-mt-2 text-micro leading-prose text-muted-foreground">A good poll replaces the tier, and the dates where the provider states them, with the provider's own.</p>
            <Field label="Plan-change command">
              <Input className="font-mono text-micro" placeholder="/path/to/change {from} {to}" value={form.changeCommand || ""} onInput={(e) => setForm({ ...form, changeCommand: e.currentTarget.value })} />
            </Field>
            <p className="-mt-2 text-micro leading-prose text-muted-foreground">One executable and its arguments, split on spaces. Not a shell line: quotes, pipes, and <code className="font-mono">&&</code> are taken as part of an argument. <code className="font-mono">{"{from}"}</code> and <code className="font-mono">{"{to}"}</code> are the tiers; with neither written, both are added at the end. With none set, the ladder names the next plan change and does not take it; a banked reset still runs, since it needs no command.</p>
            <div className="flex items-center justify-between rounded-control border px-3 py-2.5">
              <Label htmlFor={ladderId}>Ladder</Label>
              <Switch id={ladderId} checked={form.ladder} onCheckedChange={(v) => setForm({ ...form, ladder: v })} />
            </div>
            <div className="flex items-center justify-between rounded-control border px-3 py-2.5">
              <Label htmlFor={resetId}>Use a banked reset when the highest tier is spent</Label>
              <Switch id={resetId} checked={form.autoReset} onCheckedChange={(v) => setForm({ ...form, autoReset: v })} />
            </div>
            <div className="mt-2 flex flex-col gap-3 border-t pt-4">
              <h3 className="font-semibold">Login</h3>
              {login ? (
                <SignInWaiting login={login} provider={title(view.account.provider)} onCancel={onCancelSignIn} />
              ) : (
                <>
                  {canSignIn ? (
                    <>
                      <p className="text-micro leading-prose text-muted-foreground">
                        For a login that Renew can no longer refresh. A new sign-in replaces the stored login at once and does not wait for Save.
                      </p>
                      <Button type="button" variant="outline" size="sm" className="self-start" disabled={!!busy} onClick={onSignIn}>Sign in again</Button>
                    </>
                  ) : null}
                  <CredentialInput label={canSignIn ? "Or copy a credential file over it" : "Copy a fresh credential file over the stored one"} value={credential} onChange={setCredential} />
                  <Button type="button" variant="outline" size="sm" className="self-start" disabled={!!busy || !credential} onClick={() => onReplace(credential)}>
                    {busy === "replace" ? "Replacing" : "Replace credential"}
                  </Button>
                </>
              )}
            </div>
          </div>
          {/* Above the footer and outside the scroll area, so a failure is seen wherever the form is scrolled. */}
          {errorLine || formError ? (
            <div className="flex flex-col gap-2 border-t px-4 py-3">
              {errorLine}
              {formError ? <p id={errorId} className="text-micro text-destructive" role="alert">{formError}</p> : null}
            </div>
          ) : null}
          <SheetFooter className="flex-row justify-between border-t">
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button type="button" variant="ghost" className="text-destructive hover:text-destructive" disabled={!!busy}>Remove account</Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Remove {view.account.name}?</AlertDialogTitle>
                  <AlertDialogDescription>
                    This deletes <span className="break-all font-mono text-micro">{view.account.home}</span>, including the login subman stores for it. The vendor CLI's own login is not touched.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Keep account</AlertDialogCancel>
                  <AlertDialogAction variant="destructive" onClick={onDelete}>Remove account</AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
            <Button type="submit" disabled={!!busy}>{busy === "save" ? "Saving" : "Save changes"}</Button>
          </SheetFooter>
        </form>
        <AlertDialog open={confirmDiscard} onOpenChange={setConfirmDiscard}>
          <AlertDialogContent size="sm">
            <AlertDialogHeader>
              <AlertDialogTitle>Discard your changes?</AlertDialogTitle>
              <AlertDialogDescription>The edits to {view.account.name} have not been saved.</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Keep editing</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={onClose}>Discard changes</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SheetContent>
    </Sheet>
  );
}

/** What a sign-in shows while the operator is in the browser. */
function SignInWaiting({ login, provider, onCancel }: { login: LoginStart; provider: string; onCancel: () => void }) {
  return (
    <div className="flex flex-col gap-3" role="status">
      <p>Finish signing in to {provider} in your browser. This window picks it up when you are done.</p>
      {login.userCode ? (
        <p>
          The page should show this code: <span className="font-mono text-body font-semibold tracking-label">{login.userCode}</span>
        </p>
      ) : null}
      <p className="text-micro leading-prose text-muted-foreground">
        No page opened? Open this address yourself:
        <span className="mt-1 block max-h-16 overflow-auto rounded-control border bg-card px-2 py-1 font-mono text-micro break-all select-all">{login.url}</span>
      </p>
      <div className="flex gap-2">
        <Button type="button" variant="outline" size="sm" onClick={() => openExternal(login.url)}>Open the page again</Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>Cancel sign-in</Button>
      </div>
    </div>
  );
}

export function AddDialog({
  draft, providers, busy, login, errorLine, onChange, onClose, onSignIn, onCancelSignIn, onImport,
}: {

  draft: ImportRequest;
  providers: ProviderInfo[];
  /** The action in flight, if any. Submitting waits for it. */
  busy: string;
  /** The sign-in in progress, when there is one. */
  login: LoginStart | null;
  errorLine: ComponentChildren;
  onChange: (d: ImportRequest) => void;
  onClose: () => void;
  onSignIn: () => void;
  onCancelSignIn: () => void;
  onImport: () => void;
}) {
  const current = providers.find((p) => p.id === draft.provider);
  // A provider subman cannot sign in to takes a credential file; the others may.
  const [preferFile, setPreferFile] = useState(false);
  const fromFile = preferFile || !current?.login;
  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      {/* Coming back from the browser, the first click lands outside the dialog. It must not end the sign-in. */}
      <DialogContent onInteractOutside={(e) => { if (login) e.preventDefault(); }}>
        <DialogHeader>
          <DialogTitle>Add account</DialogTitle>
          <DialogDescription>
            {fromFile ? "The credential file is copied. The vendor CLI keeps its own." : "Sign in from here. subman keeps its own login and leaves the vendor CLI's alone."}
          </DialogDescription>
        </DialogHeader>
        {login ? (
          <SignInWaiting login={login} provider={current?.title ?? draft.provider} onCancel={onCancelSignIn} />
        ) : (
          <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (fromFile) onImport(); else onSignIn(); }}>
            {errorLine}
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Name, optional"><Input placeholder="the login's email" value={draft.name} onInput={(e) => onChange({ ...draft, name: e.currentTarget.value })} /></Field>
              <Field label="Provider">
                <NativeSelect
                  value={draft.provider}
                  onChange={(e) => {
                    const next = providers.find((p) => p.id === e.currentTarget.value);
                    if (!next) return;
                    // The path follows the provider unless the operator typed their own.
                    const untouched = draft.credentialPath === "" || draft.credentialPath === (current?.credentialPath ?? "");
                    onChange({
                      ...draft,
                      provider: next.id,
                      tierId: "",
                      credentialPath: untouched ? next.credentialPath ?? "" : draft.credentialPath,
                    });
                  }}
                >
                  {providers.map((p) => <NativeSelectOption key={p.id} value={p.id}>{p.title}</NativeSelectOption>)}
                </NativeSelect>
              </Field>
            </div>
            <p className="-mt-1 text-micro leading-prose text-muted-foreground">
              The tier is read from the provider on the first poll, and so are the activation and renewal dates where the provider states them. Anything it does not state can be set in Edit.
            </p>
            {fromFile ? (
              <>
                <CredentialInput label="Credential file" required value={draft.credentialPath} onChange={(credentialPath) => onChange({ ...draft, credentialPath })} />
                <p className="-mt-2 text-micro leading-prose text-muted-foreground">
                  {current?.login
                    ? `The file the ${current.title} CLI wrote when you signed in there.`
                    : `${current?.title ?? "This provider"} has no sign-in here, so point at the file its CLI wrote when you signed in there.`}
                </p>
              </>
            ) : null}
            <DialogFooter className="sm:justify-between">
              {current?.login ? (
                <Button type="button" variant="link" className="px-0 text-muted-foreground" onClick={() => setPreferFile(!preferFile)}>
                  {preferFile ? "Sign in with the browser instead" : "Use a credential file instead"}
                </Button>
              ) : <span />}
              <span className="flex gap-2">
                <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
                <Button type="submit" disabled={!!busy}>{fromFile ? (busy === "add" ? "Adding" : "Add account") : `Sign in to ${current?.title ?? ""}`}</Button>
              </span>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * A labelled path field with a button that opens the native file dialog
 * beside the current path. The label names the input alone: the button and
 * the failure text sit outside it.
 */
function CredentialInput({ label, value, required, onChange }: { label: string; value: string; required?: boolean; onChange: (path: string) => void }) {
  const id = useId();
  const failureId = useId();
  const [failure, setFailure] = useState("");
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="font-normal text-muted-foreground">{label}</Label>
      <div className="flex gap-2">
        <Input id={id} className="font-mono text-micro" required={required} aria-invalid={failure ? true : undefined} aria-describedby={failure ? failureId : undefined} value={value} onInput={(e) => onChange(e.currentTarget.value)} />
        <Button
          type="button"
          variant="outline"
          className="shrink-0"
          onClick={async () => {
            setFailure("");
            try {
              const picked = await api.pickCredential(value);
              if (picked) onChange(picked);
            } catch (err) {
              setFailure(messageOf(err));
            }
          }}
        >
          <FolderOpen />
          Browse
        </Button>
      </div>
      {failure ? <p id={failureId} className="text-micro text-destructive" role="alert">The file dialog did not open: {failure}. Type the path instead.</p> : null}
    </div>
  );
}

/**
 * A day as YYYY-MM-DD text. Not type="date": WebKitGTK draws an empty date
 * input as today's date, which reads as a value that was never entered.
 */
function DayInput({ name, value, invalid, describedBy, onChange }: { name: string; value: string; invalid?: boolean; describedBy?: string; onChange: (day: string) => void }) {
  return (
    <Input
      name={name}
      type="text"
      inputMode="numeric"
      placeholder="YYYY-MM-DD"
      pattern="\d{4}-\d{2}-\d{2}"
      title="A date as YYYY-MM-DD"
      aria-invalid={invalid || undefined}
      aria-describedby={describedBy}
      value={value}
      onInput={(e) => onChange(e.currentTarget.value.replace(/^(\d{4}-\d{2}-\d{2})[T ].*/, "$1"))}
    />
  );
}

function Field({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <Label className="flex-col items-stretch gap-1.5 font-normal text-muted-foreground">
      {label}
      {children}
    </Label>
  );
}
