"use client";

// R7 — pintu masuk kecil ke Command Palette (dashboard, pojok kanan atas).
// Shortcut Ctrl+K / "/" tetap jalan dari mana pun; tombol ini untuk staf baru.
export function GlobalSearchButton() {
  return (
    <button
      onClick={() => window.dispatchEvent(new Event("open-command-palette"))}
      className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface
        px-2.5 py-1.5 text-xs text-text-tertiary hover:border-accent hover:text-text-secondary
        transition-colors shrink-0"
      aria-label="Buka pencarian global"
      title="Cari halaman, unit, atau customer (Ctrl+K)"
    >
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
        strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="11" cy="11" r="8" /><path d="M21 21l-4.35-4.35" />
      </svg>
      <span>Cari</span>
      <kbd className="rounded border border-border bg-bg px-1 py-0.5 text-[9px] font-mono">Ctrl+K</kbd>
    </button>
  );
}
