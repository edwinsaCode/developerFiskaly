"use client";

import { createContext, useContext, ReactNode } from "react";
import type { User } from "@/lib/types/api";

const UserContext = createContext<User | null>(null);

export function UserProvider({ user, children }: { user: User; children: ReactNode }) {
  return <UserContext.Provider value={user}>{children}</UserContext.Provider>;
}

/** Returns the authenticated user from context. Throws if used outside AppShell. */
export function useUser(): User {
  const user = useContext(UserContext);
  if (!user) throw new Error("useUser must be used within UserProvider");
  return user;
}

/** Returns the user or null — safe to call anywhere. */
export function useUserSafe(): User | null {
  return useContext(UserContext);
}
