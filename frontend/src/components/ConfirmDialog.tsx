import { useCallback, useEffect, useRef, useState } from "react";
import { createContext, useContext, useMemo } from "react";
import type { ReactNode } from "react";

interface ConfirmRequest {
  title: string;
  body: string;
  variant?: "danger" | "primary";
  resolve: (ok: boolean) => void;
}

interface ConfirmContext {
  ask: (title: string, body: string, variant?: "danger" | "primary") => Promise<boolean>;
}

const Ctx = createContext<ConfirmContext | null>(null);

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [req, setReq] = useState<ConfirmRequest | null>(null);
  const dlgRef = useRef<HTMLDialogElement | null>(null);

  const ask = useCallback(
    (title: string, body: string, variant: "danger" | "primary" = "danger") =>
      new Promise<boolean>((resolve) => {
        setReq({ title, body, variant, resolve });
      }),
    [],
  );

  useEffect(() => {
    if (req && dlgRef.current && !dlgRef.current.open) {
      dlgRef.current.showModal();
    }
  }, [req]);

  const close = (ok: boolean) => {
    if (!req) return;
    req.resolve(ok);
    dlgRef.current?.close();
    setReq(null);
  };

  const value = useMemo(() => ({ ask }), [ask]);

  return (
    <Ctx.Provider value={value}>
      {children}
      <dialog ref={dlgRef} onCancel={(e) => { e.preventDefault(); close(false); }}>
        <h3 style={{ marginTop: 0 }}>{req?.title ?? "Confirm"}</h3>
        <p>{req?.body}</p>
        <menu>
          <button onClick={() => close(false)}>Cancel</button>
          <button className={req?.variant ?? "danger"} onClick={() => close(true)}>
            Confirm
          </button>
        </menu>
      </dialog>
    </Ctx.Provider>
  );
}

export function useConfirm() {
  const c = useContext(Ctx);
  if (!c) throw new Error("useConfirm must be used inside <ConfirmProvider>");
  return c;
}
