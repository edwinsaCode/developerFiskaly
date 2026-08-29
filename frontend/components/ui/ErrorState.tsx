"use client";

import { ReactNode } from "react";

interface ErrorStateProps {
  title?: string;
  description?: string;
  /** Called on "Coba Lagi" click. Falls back to window.location.reload() if omitted. */
  onRetry?: () => void;
  /** Show "Coba Lagi" button (default true). */
  showRetry?: boolean;
  action?: ReactNode;
  className?: string;
}

export function ErrorState({
  title = "Terjadi Kesalahan",
  description,
  onRetry,
  showRetry = true,
  action,
  className = "",
}: ErrorStateProps) {
  function handleRetry() {
    if (onRetry) {
      onRetry();
    } else {
      window.location.reload();
    }
  }

  return (
    <div className={`flex flex-col items-center justify-center py-12 px-6 text-center ${className}`}>
      <div className="mb-3 text-danger">
        <svg width="36" height="36" viewBox="0 0 24 24" fill="none"
          stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round"
          aria-hidden="true">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
      </div>
      <p className="text-sm font-semibold text-text-primary mb-1">{title}</p>
      {description && (
        <p className="text-sm text-text-secondary max-w-xs mb-4">{description}</p>
      )}
      {(showRetry || action) && (
        <div className="flex items-center gap-2 mt-2">
          {showRetry && (
            <button
              onClick={handleRetry}
              className="text-sm px-3 py-1.5 rounded border border-border text-text-secondary
                hover:text-accent hover:border-accent/40 transition-colors"
            >
              Coba Lagi
            </button>
          )}
          {action}
        </div>
      )}
    </div>
  );
}
