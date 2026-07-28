import { LockKeyhole, ShieldAlert, UnlockKeyhole } from "lucide-react";
import { type FormEvent, useState } from "react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useIsMobile } from "@/hooks/use-mobile";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { cn } from "@/lib/utils";

type VaultState = "locked" | "unlocked" | "unavailable";

export function LocalVaultControl() {
  const settings = useWorkspaceSettingsData();
  const isMobile = useIsMobile();
  const [open, setOpen] = useState(false);
  const [passphrase, setPassphrase] = useState("");
  const [error, setError] = useState("");
  const vault = settings.workspaceConfig?.secret_vault;

  if (!vault || vault.state === "uninitialized") {
    return null;
  }

  const state = normalizeVaultState(vault.state);
  const close = () => {
    setOpen(false);
    setPassphrase("");
    setError("");
  };
  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen);
    if (!nextOpen) {
      setPassphrase("");
      setError("");
    }
  };
  const unlock = async (event: FormEvent) => {
    event.preventDefault();
    if (!passphrase || settings.workspaceConfigBusy) {
      return;
    }
    setError("");
    try {
      await settings.handleUnlockLocalVault(passphrase);
      close();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not unlock the encrypted vault.");
    }
  };
  const lock = async () => {
    if (settings.workspaceConfigBusy) {
      return;
    }
    setError("");
    try {
      await settings.handleLockLocalVault();
      close();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not lock the encrypted vault.");
    }
  };
  const trigger = (
    <Button
      aria-label={`Encrypted vault ${state}`}
      variant="ghost"
      size="icon"
      className="mr-1 text-zinc-300 hover:bg-zinc-800 hover:text-white"
    >
      <VaultLockIcon state={state} busy={settings.workspaceConfigBusy} />
    </Button>
  );
  const content = (
    <VaultSessionForm
      state={state}
      message={vault.message}
      secretCount={vault.secret_count}
      passphrase={passphrase}
      error={error}
      busy={settings.workspaceConfigBusy}
      mobile={isMobile}
      onPassphraseChange={setPassphrase}
      onLock={() => void lock()}
      onCancel={close}
    />
  );

  if (isMobile) {
    return (
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <Tooltip>
          <TooltipTrigger asChild>
            <DialogTrigger asChild>{trigger}</DialogTrigger>
          </TooltipTrigger>
          <TooltipContent>Encrypted vault: {state}</TooltipContent>
        </Tooltip>
        <DialogContent>
          <form className="flex flex-col gap-4" onSubmit={unlock}>
            <DialogHeader>
              <DialogTitle>Encrypted vault</DialogTitle>
              <DialogDescription>{vaultDescription(state)}</DialogDescription>
            </DialogHeader>
            {content}
          </form>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>{trigger}</PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>Encrypted vault: {state}</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" aria-label="Encrypted vault" className="w-80">
        <form className="flex flex-col gap-4" onSubmit={unlock}>
          <PopoverHeader>
            <PopoverTitle>Encrypted vault</PopoverTitle>
            <PopoverDescription>{vaultDescription(state)}</PopoverDescription>
          </PopoverHeader>
          {content}
        </form>
      </PopoverContent>
    </Popover>
  );
}

function VaultSessionForm({
  state,
  message,
  secretCount,
  passphrase,
  error,
  busy,
  mobile,
  onPassphraseChange,
  onLock,
  onCancel,
}: {
  state: VaultState;
  message?: string;
  secretCount: number;
  passphrase: string;
  error: string;
  busy: boolean;
  mobile: boolean;
  onPassphraseChange: (value: string) => void;
  onLock: () => void;
  onCancel: () => void;
}) {
  if (state === "unavailable") {
    return (
      <Alert variant="destructive">
        <ShieldAlert />
        <AlertTitle>Vault unavailable</AlertTitle>
        <AlertDescription>
          {message || "Renart cannot access this encrypted vault."}
        </AlertDescription>
      </Alert>
    );
  }

  if (state === "unlocked") {
    return (
      <>
        <div className="flex items-center gap-3">
          <VaultLockAnimation state={state} />
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <div className="flex items-center gap-2">
              <span className="font-medium">Unlocked for this session</span>
              <Badge variant="secondary">
                {secretCount} secret{secretCount === 1 ? "" : "s"}
              </Badge>
            </div>
            <p className="text-muted-foreground">{message}</p>
          </div>
        </div>
        <Button type="button" variant="outline" disabled={busy} onClick={onLock}>
          {busy ? <Spinner data-icon="inline-start" /> : <LockKeyhole data-icon="inline-start" />}
          Lock vault
        </Button>
        {error ? (
          <Alert variant="destructive">
            <ShieldAlert />
            <AlertTitle>Could not lock vault</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
      </>
    );
  }

  return (
    <>
      <div className="flex items-center gap-3">
        <VaultLockAnimation state={state} />
        <p className="min-w-0 flex-1 text-muted-foreground">{message}</p>
      </div>
      <FieldGroup>
        <Field data-invalid={Boolean(error)}>
          <FieldLabel htmlFor="header-local-vault-passphrase">Passphrase</FieldLabel>
          <Input
            id="header-local-vault-passphrase"
            type="password"
            autoComplete="current-password"
            value={passphrase}
            aria-invalid={Boolean(error)}
            disabled={busy}
            autoFocus
            onChange={(event) => onPassphraseChange(event.target.value)}
          />
          <FieldError>{error}</FieldError>
        </Field>
      </FieldGroup>
      {mobile ? (
        <DialogFooter>
          <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button type="submit" disabled={!passphrase || busy}>
            {busy ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <UnlockKeyhole data-icon="inline-start" />
            )}
            Unlock vault
          </Button>
        </DialogFooter>
      ) : (
        <Button type="submit" disabled={!passphrase || busy}>
          {busy ? <Spinner data-icon="inline-start" /> : <UnlockKeyhole data-icon="inline-start" />}
          Unlock vault
        </Button>
      )}
    </>
  );
}

function VaultLockIcon({ state, busy }: { state: VaultState; busy: boolean }) {
  const unlocked = state === "unlocked";
  return (
    <span
      className={cn(
        "relative flex size-4 items-center justify-center",
        busy ? "animate-pulse motion-reduce:animate-none" : null,
      )}
      aria-hidden
    >
      <LockKeyhole
        className={cn(
          "absolute transition-[opacity,transform] duration-300 motion-reduce:transition-none",
          unlocked ? "-rotate-12 scale-75 opacity-0" : "rotate-0 scale-100 opacity-100",
        )}
      />
      <UnlockKeyhole
        className={cn(
          "absolute transition-[opacity,transform] duration-300 motion-reduce:transition-none",
          unlocked ? "rotate-0 scale-100 opacity-100" : "rotate-12 scale-75 opacity-0",
        )}
      />
    </span>
  );
}

function VaultLockAnimation({ state }: { state: "locked" | "unlocked" }) {
  const unlocked = state === "unlocked";
  return (
    <div className="relative flex size-11 shrink-0 items-center justify-center rounded-full bg-muted">
      <LockKeyhole
        className={cn(
          "absolute size-5 transition-[opacity,transform] duration-300 motion-reduce:transition-none",
          unlocked ? "-translate-y-1 -rotate-12 scale-75 opacity-0" : "opacity-100",
        )}
      />
      <UnlockKeyhole
        className={cn(
          "absolute size-5 transition-[opacity,transform] duration-300 motion-reduce:transition-none",
          unlocked ? "opacity-100" : "translate-y-1 rotate-12 scale-75 opacity-0",
        )}
      />
    </div>
  );
}

function normalizeVaultState(state: string): VaultState {
  if (state === "unlocked") {
    return "unlocked";
  }
  if (state === "locked") {
    return "locked";
  }
  return "unavailable";
}

function vaultDescription(state: VaultState) {
  if (state === "unlocked") {
    return "Connection credentials are available to this Renart process.";
  }
  if (state === "locked") {
    return "Unlock the vault to use its stored connection credentials.";
  }
  return "Renart cannot access the encrypted vault on this device.";
}
