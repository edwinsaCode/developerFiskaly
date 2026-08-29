"use client";

import { createContext, useContext, useCallback, useState, ReactNode, useEffect } from "react";
import { createPortal } from "react-dom";

type ToastVariant = "success" | "error" | "warning" | "info";

interface ToastItem {
  id: string;
  message: string;
  variant: ToastVariant;
}

interface ToastContextValue {
  toast: (message: string, variant?: ToastVariant) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

const variantStyles: Record<ToastVariant, string> = {
  success: "border-l-4 border-success bg-success-bg text-success",
  error:   "border-l-4 border-danger bg-danger-bg text-danger",
  warning: "border-l-4 border-warning bg-warning-bg text-warning",
  info:    "border-l-4 border-accent bg-accent-light text-accent",
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  // Portal baru boleh dipasang setelah mount: document tidak ada saat SSR.
  const [mounted, setMounted] = useState(false);
  useEffect(() => { setMounted(true); }, []);

  const toast = useCallback((message: string, variant: ToastVariant = "info") => {
    const id = `${Date.now()}-${Math.random()}`;
    setItems(prev => [...prev, { id, message, variant }]);
    setTimeout(() => {
      setItems(prev => prev.filter(t => t.id !== id));
    }, 4000);
  }, []);

  // Dua hal yang membuat toast selalu terbaca, dan keduanya perlu:
  //
  //   1. z-toast — lapisan di atas z-overlay (lihat tailwind.config.ts).
  //      Tanpa ini modal dan toast berada di angka yang sama dan urutan DOM
  //      yang menentukan; modal mem-portal dirinya belakangan, jadi menang.
  //   2. portal ke <body> — z-index hanya berlaku dalam stacking context-nya
  //      sendiri. Selama toast dirender di tengah pohon React, satu ancestor
  //      ber-transform/filter/backdrop-blur sudah cukup untuk mengurungnya di
  //      bawah modal, berapa pun angkanya.
  const layer = (
    <div
      className="fixed bottom-4 right-4 z-toast flex flex-col gap-2 pointer-events-none"
      role="status"
      aria-live="polite"
    >
      {items.map(item => (
        <div
          key={item.id}
          className={`pointer-events-auto flex items-start gap-3 rounded shadow-lg px-4 py-3 text-sm font-medium
            max-w-sm animate-in slide-in-from-right-4 fade-in duration-200
            ${variantStyles[item.variant]}`}
        >
          <span className="flex-1">{item.message}</span>
          <button
            onClick={() => setItems(prev => prev.filter(t => t.id !== item.id))}
            className="shrink-0 opacity-60 hover:opacity-100 transition-opacity text-base leading-none"
            aria-label="Tutup"
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      {mounted && createPortal(layer, document.body)}
    </ToastContext.Provider>
  );
}

export function useToast() {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast harus dipanggil dalam ToastProvider");
  return ctx;
}
