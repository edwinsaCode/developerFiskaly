"use client";

import { ReactNode, useCallback, useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title?: string;
  /** Kalimat penjelas di bawah judul — konteks yang tak muat di judul. */
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  /** Konten kiri pada footer (mis. catatan atau aksi sekunder). */
  footerLeft?: ReactNode;
  size?: "sm" | "md" | "lg" | "xl";
}

// Skala lebar dialog.
//
// Sebelumnya sm/md/lg = 384/512/672px: form isian jadi kurus dan label
// terpaksa menumpuk satu kolom sekalipun layarnya lega. Angka di bawah dipilih
// dari isi, bukan dari selera:
//   sm — konfirmasi & form 1–2 field; masih nyaman dibaca sekali pandang
//   md — form isian normal, dua kolom
//   lg — form dengan tabel/rincian di dalamnya
//   xl — layar kerja (alokasi pembayaran, wizard) yang memuat tabel penuh
const sizeClasses = {
  sm: "max-w-md", // 448px
  md: "max-w-2xl", // 672px
  lg: "max-w-4xl", // 896px
  xl: "max-w-6xl", // 1152px
};

const FOCUSABLE =
  'a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),[tabindex]:not([tabindex="-1"])';

export function Modal({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  footerLeft,
  size = "md",
}: ModalProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const descId = useId();

  // Fokus dikembalikan ke elemen pemicu saat dialog ditutup. Tanpa ini,
  // fokus jatuh ke <body> dan pemakai keyboard harus menelusuri halaman dari
  // awal setiap kali menutup modal.
  const restoreTo = useRef<HTMLElement | null>(null);

  // onClose disimpan di ref, dan efek di bawah hanya bergantung pada `open`.
  //
  // Ini bukan gaya penulisan — ini perbaikan bug. Sebelumnya efek fokus ikut
  // bergantung pada handler keydown, yang bergantung pada `onClose`. Hampir
  // semua pemanggil mengoper `onClose={() => setOpen(false)}` — arrow inline,
  // identitasnya baru di SETIAP render induk. Jadi setiap satu huruf yang
  // diketik memicu render, identitas baru, efek dijalankan ulang, dan fokus
  // dilempar balik ke isian pertama. Gejalanya: "ketik satu huruf, kursor
  // lompat ke field pertama" — di semua form, bukan hanya satu.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  });

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onCloseRef.current();
        return;
      }
      if (e.key !== "Tab" || !panelRef.current) return;

      // Jebak fokus di dalam panel: dialog yang membiarkan Tab keluar ke
      // halaman di belakangnya membuat pemakai mengisi form yang tak terlihat.
      const nodes = Array.from(
        panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE),
      ).filter((el) => el.offsetParent !== null || el === document.activeElement);
      if (nodes.length === 0) return;

      const first = nodes[0];
      const last = nodes[nodes.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    },
    [], // stabil selamanya — onClose dibaca lewat ref saat kejadian, bukan saat render
  );

  useEffect(() => {
    if (!open) return;

    restoreTo.current = document.activeElement as HTMLElement | null;
    document.addEventListener("keydown", handleKeyDown);

    // Kunci scroll halaman di belakang. Padding pengganti lebar scrollbar
    // supaya layout tidak "melompat" saat dialog dibuka.
    const { body } = document;
    const prevOverflow = body.style.overflow;
    const prevPadding = body.style.paddingRight;
    const gap = window.innerWidth - document.documentElement.clientWidth;
    body.style.overflow = "hidden";
    if (gap > 0) body.style.paddingRight = `${gap}px`;

    // Fokus ke isian pertama; kalau tidak ada, ke panelnya.
    const target =
      panelRef.current?.querySelector<HTMLElement>(
        'input:not([type="hidden"]):not([disabled]),select:not([disabled]),textarea:not([disabled])',
      ) ?? panelRef.current;
    target?.focus({ preventScroll: true });

    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      body.style.overflow = prevOverflow;
      body.style.paddingRight = prevPadding;
      restoreTo.current?.focus?.({ preventScroll: true });
    };
    // HANYA `open`. Menambah dependency apa pun di sini mengembalikan bug
    // "kursor lompat ke field pertama": efek ini memindahkan fokus, jadi ia
    // harus berjalan saat dialog dibuka/ditutup — bukan saat isinya berubah.
    // handleKeyDown sengaja dibuat stabil ([] di useCallback) agar aman.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  if (!open) return null;

  return createPortal(
    <div
      className="fixed inset-0 z-overlay flex items-start justify-center overflow-y-auto p-4 sm:p-6"
      aria-modal="true"
      role="dialog"
      aria-labelledby={title ? titleId : undefined}
      aria-describedby={description ? descId : undefined}
    >
      {/* backdrop */}
      <div
        className="fixed inset-0 bg-text-primary/40 backdrop-blur-[2px] animate-overlay-in motion-reduce:animate-none"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* panel — my-auto membuatnya di tengah saat pendek, dan tetap bisa
          di-scroll dari atas saat isinya panjang di layar rendah. */}
      <div
        ref={panelRef}
        tabIndex={-1}
        className={`relative z-10 my-auto w-full ${sizeClasses[size]} bg-surface border border-border
          rounded-xl shadow-dialog flex flex-col max-h-[calc(100vh-2rem)] sm:max-h-[calc(100vh-3rem)]
          outline-none animate-panel-in motion-reduce:animate-none`}
      >
        {title && (
          <div className="flex items-start justify-between gap-4 px-6 py-4 sm:px-7 sm:py-5 border-b border-border shrink-0">
            <div className="min-w-0">
              <h2 id={titleId} className="text-lg font-semibold text-text-primary leading-tight">
                {title}
              </h2>
              {description && (
                <p id={descId} className="text-sm text-text-secondary mt-1">
                  {description}
                </p>
              )}
            </div>
            <button
              type="button"
              onClick={onClose}
              className="shrink-0 -mr-1.5 -mt-1 h-8 w-8 grid place-items-center rounded-lg text-text-tertiary
                hover:text-text-primary hover:bg-border-subtle transition-colors
                focus:outline-none focus:ring-2 focus:ring-accent/30"
              aria-label="Tutup"
            >
              <svg
                viewBox="0 0 20 20"
                className="h-4 w-4"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.8"
                strokeLinecap="round"
              >
                <path d="M5 5l10 10M15 5L5 15" />
              </svg>
            </button>
          </div>
        )}

        <div className="flex-1 overflow-y-auto px-6 py-5 sm:px-7 sm:py-6">{children}</div>

        {(footer || footerLeft) && (
          <div
            className="px-6 py-4 sm:px-7 border-t border-border shrink-0 bg-border-subtle/30 rounded-b-xl
              flex flex-wrap items-center justify-between gap-3"
          >
            <div className="text-xs text-text-secondary min-w-0">{footerLeft}</div>
            <div className="flex items-center justify-end gap-3 ml-auto">{footer}</div>
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}
