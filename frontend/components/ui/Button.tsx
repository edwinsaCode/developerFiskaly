"use client";

import { ButtonHTMLAttributes } from "react";

type Variant = "primary" | "secondary" | "ghost" | "danger";
type Size = "sm" | "md" | "lg";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
}

const variantClasses: Record<Variant, string> = {
  primary:
    "bg-accent text-white hover:bg-accent-hover focus-visible:ring-2 focus-visible:ring-accent/50 disabled:opacity-50",
  secondary:
    "bg-surface text-text-primary border border-border hover:bg-border-subtle focus-visible:ring-2 focus-visible:ring-accent/30 disabled:opacity-50",
  ghost:
    "text-text-secondary hover:text-accent hover:bg-accent/10 focus-visible:ring-2 focus-visible:ring-accent/30 disabled:opacity-50",
  danger:
    "bg-danger text-white hover:opacity-90 focus-visible:ring-2 focus-visible:ring-danger/40 disabled:opacity-50",
};

// Tinggi dinaikkan satu tingkat agar sejajar dengan kotak isian (40px). Tombol
// 32px di samping field 40px terbaca sebagai elemen yang tidak sengaja
// dirancang bersama — dan tombol aksi utama dialog jadi tampak ragu.
const sizeClasses: Record<Size, string> = {
  sm: "h-8 px-3.5 text-xs",
  md: "h-9 px-4 text-sm",
  lg: "h-10 px-5 text-base",
};

export function Button({
  variant = "primary",
  size = "md",
  loading = false,
  className = "",
  children,
  disabled,
  ...props
}: ButtonProps) {
  return (
    <button
      className={`inline-flex items-center justify-center gap-2 rounded-lg font-medium whitespace-nowrap
        transition-colors duration-100 cursor-pointer outline-none select-none
        disabled:cursor-not-allowed
        ${variantClasses[variant]} ${sizeClasses[size]} ${className}`}
      disabled={disabled || loading}
      {...props}
    >
      {loading && (
        <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
      )}
      {children}
    </button>
  );
}
