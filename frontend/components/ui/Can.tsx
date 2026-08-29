"use client";

import { ReactNode } from "react";
import { useUserSafe } from "@/lib/context/UserContext";
import type { User } from "@/lib/types/api";

type Role = User["role"];

interface CanProps {
  /** Roles that are allowed to see the children. */
  roles: Role[];
  children: ReactNode;
  /** Rendered when the user's role is not in `roles`. Defaults to null (hidden). */
  fallback?: ReactNode;
}

/**
 * PermissionGate — renders children only when the logged-in user's role
 * is in the `roles` array. Use for client-side UI gating only;
 * backend endpoints enforce their own authorization.
 *
 * @example
 * <Can roles={["owner", "accountant"]}>
 *   <Button>Simpan</Button>
 * </Can>
 *
 * <Can roles={["owner"]} fallback={<p>Hanya owner.</p>}>
 *   <DangerousAction />
 * </Can>
 */
export function Can({ roles, children, fallback = null }: CanProps) {
  const user = useUserSafe();
  if (!user) return <>{fallback}</>;
  if (!roles.includes(user.role)) return <>{fallback}</>;
  return <>{children}</>;
}

/** Convenience alias for <Can roles={["owner","accountant"]}> */
export function CanWrite({ children, fallback }: { children: ReactNode; fallback?: ReactNode }) {
  return (
    <Can roles={["owner", "accountant"]} fallback={fallback}>
      {children}
    </Can>
  );
}

/** Convenience alias for <Can roles={["owner"]}> */
export function CanOwner({ children, fallback }: { children: ReactNode; fallback?: ReactNode }) {
  return (
    <Can roles={["owner"]} fallback={fallback}>
      {children}
    </Can>
  );
}
