import { useState } from "react";
import type { SSHHost } from "../api/types";

interface Props {
  hosts: SSHHost[];
  onAdd: (host: Record<string, unknown>) => Promise<SSHHost>;
  onRemove: (id: string) => Promise<void>;
}

export function SSHHosts({ hosts, onAdd, onRemove }: Props) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState({
    id: "",
    name: "",
    address: "",
    port: "22",
    user: "",
    identity_file: "",
    known_hosts_file: "",
    base_folder: "",
    agent_command: "rcod-agent",
  });

  const update = (key: keyof typeof form, value: string) => setForm((current) => ({ ...current, [key]: value }));

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      await onAdd({ ...form, port: Number(form.port) || 22 });
      setOpen(false);
      window.location.reload();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    if (!window.confirm(`Remove SSH host ${id}?`)) return;
    setBusy(true);
    try {
      await onRemove(id);
      window.location.reload();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="sidebar-section sidebar-hosts">
      <div className="sidebar-section__header">
        <span>SSH hosts</span>
        <button type="button" onClick={() => setOpen((value) => !value)} aria-label="Add SSH host">+</button>
      </div>
      {hosts.length === 0 && !open && <div className="sidebar-empty">No remote hosts</div>}
      <div className="sidebar-section__list">
        {hosts.map((host) => (
          <div key={host.identity.id} className="session-chip ssh-host-chip">
            <div className="session-chip__title">
              <span>{host.identity.name || host.identity.id}</span>
              <span className={`session-chip__live ssh-host-status ssh-host-status--${host.status}`}>{host.status}</span>
            </div>
            <div className="session-chip__meta">
              <span>{host.identity.user ? `${host.identity.user}@` : ""}{host.identity.address}:{host.identity.port}</span>
              <button type="button" onClick={() => void remove(host.identity.id)} disabled={busy}>remove</button>
            </div>
            {host.last_error && <div className="text-[10px] text-red-300 truncate" title={host.last_error}>{host.last_error}</div>}
          </div>
        ))}
      </div>
      {open && (
        <div className="ssh-host-form">
          {(["id", "name", "address", "port", "user", "identity_file", "known_hosts_file", "base_folder", "agent_command"] as const).map((key) => (
            <input
              key={key}
              value={form[key]}
              onChange={(event) => update(key, event.target.value)}
              placeholder={key.replace(/_/g, " ")}
              aria-label={key}
              type={key === "port" ? "number" : "text"}
            />
          ))}
          <button type="button" onClick={() => void submit()} disabled={busy || !form.id || !form.address || !form.base_folder}>
            {busy ? "Connecting..." : "Add host"}
          </button>
          {error && <div className="text-[10px] text-red-300">{error}</div>}
        </div>
      )}
    </section>
  );
}
