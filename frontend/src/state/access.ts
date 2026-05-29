import { createContext, useContext } from "react";

// CanWriteContext carries whether the current session may issue mutating
// requests. Defaults to true so any component rendered outside a provider
// (and every auth-disabled deployment) behaves exactly as before roles
// existed. The Shell lowers it to false for read-only sessions, which hides
// the destructive controls (the server enforces the same gate regardless).
const CanWriteContext = createContext<boolean>(true);

export const CanWriteProvider = CanWriteContext.Provider;

export function useCanWrite(): boolean {
  return useContext(CanWriteContext);
}
