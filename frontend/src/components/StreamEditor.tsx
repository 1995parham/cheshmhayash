import { json } from "@codemirror/lang-json";
import { oneDark } from "@codemirror/theme-one-dark";
import CodeMirror from "@uiw/react-codemirror";
import { X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { useToast } from "../state/toast";
import type { StreamConfig } from "../types";
import { useConfirm } from "./ConfirmDialog";

interface Props {
  cluster: string;
  stream: string;
  initialConfig: StreamConfig;
  onClose: () => void;
  onSaved: () => void;
}

export function StreamEditor({ cluster, stream, initialConfig, onClose, onSaved }: Props) {
  const dlgRef = useRef<HTMLDialogElement | null>(null);
  const originalRef = useRef(JSON.stringify(initialConfig, null, 2));
  const [value, setValue] = useState(originalRef.current);
  const [err, setErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const confirm = useConfirm();
  const toast = useToast();

  useEffect(() => {
    if (dlgRef.current && !dlgRef.current.open) dlgRef.current.showModal();
  }, []);

  function close() {
    dlgRef.current?.close();
    onClose();
  }

  function reset() {
    setValue(originalRef.current);
    setErr(null);
  }

  function format() {
    try {
      setValue(JSON.stringify(JSON.parse(value), null, 2));
      setErr(null);
    } catch (e) {
      setErr(`JSON parse error: ${(e as Error).message}`);
    }
  }

  async function save() {
    setErr(null);
    let cfg: unknown;
    try {
      cfg = JSON.parse(value);
    } catch (e) {
      setErr(`JSON parse error: ${(e as Error).message}`);
      return;
    }
    if (!cfg || typeof cfg !== "object" || Array.isArray(cfg)) {
      setErr("config must be a JSON object");
      return;
    }
    const named = cfg as { name?: unknown };
    if (typeof named.name === "string" && named.name !== stream) {
      setErr(`config.name (${named.name}) must match stream (${stream})`);
      return;
    }
    if (value === originalRef.current) {
      if (
        !(await confirm.ask("No changes", "The config is unchanged. Send PUT anyway?", "primary"))
      )
        return;
    }
    if (
      !(await confirm.ask(
        "Update stream",
        `Send this config update to ${stream}? Some fields cannot change without recreate.`,
        "primary",
      ))
    ) {
      return;
    }
    setSaving(true);
    try {
      const r = (await api.updateStream(cluster, stream, cfg)) as {
        error?: { code: number; err_code: number; description?: string };
      };
      if (r?.error) {
        setErr(`NATS rejected: ${r.error.code} (${r.error.err_code}) ${r.error.description ?? ""}`);
        return;
      }
      toast.push(`${stream} updated`, "ok");
      close();
      onSaved();
    } catch (e) {
      setErr(`request failed: ${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }

  return (
    <dialog
      ref={dlgRef}
      className="edit-dialog"
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <header className="edit-head">
        <div>
          <div className="title">Edit stream config</div>
          <div className="sub">
            {stream} · edit &amp; Save to PUT <code>$JS.API.STREAM.UPDATE</code>
          </div>
        </div>
        <div className="spacer" style={{ flex: 1 }}></div>
        <button className="close-btn" onClick={close} aria-label="cancel">
          <X size={18} />
        </button>
      </header>
      <div className="edit-body">
        <p className="muted edit-hint">
          NATS accepts a full <code>StreamConfig</code>. Some fields cannot change without
          recreating the stream (e.g. <code>storage</code>, <code>retention</code>). Make sure{" "}
          <code>name</code> matches.
        </p>
        <div className="editor">
          <CodeMirror
            value={value}
            theme={oneDark}
            extensions={[json()]}
            onChange={(v) => setValue(v)}
            basicSetup={{
              lineNumbers: true,
              foldGutter: true,
              highlightActiveLine: true,
              indentOnInput: true,
              bracketMatching: true,
              closeBrackets: true,
            }}
            height="100%"
            style={{ height: "100%" }}
          />
        </div>
        <div className="edit-err">{err}</div>
      </div>
      <menu className="edit-actions">
        <button type="button" onClick={reset}>
          Reset
        </button>
        <button type="button" onClick={format}>
          Format
        </button>
        <div style={{ flex: 1 }}></div>
        <button type="button" onClick={close}>
          Cancel
        </button>
        <button type="button" className="primary" onClick={save} disabled={saving}>
          {saving ? "Saving…" : "Save"}
        </button>
      </menu>
    </dialog>
  );
}
