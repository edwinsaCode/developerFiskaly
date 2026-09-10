import { Fragment, ReactNode } from "react";
import { DocumentNumberLink } from "./DocumentNumberLink";

// Bentuk nomor dokumen W-2: {PREFIX}/{TAHUN}/{URUT}, mis. BKK/2026/000002.
// Prefix tidak di-hardcode ke jenis tertentu — admin bisa menambah jenis baru
// kapan saja (lihat document.ts) — jadi polanya generik, bukan daftar kode.
const DOCUMENT_NUMBER_RE = /\b[A-Z]{2,6}\/\d{4}\/\d{4,10}\b/g;

interface Props {
  token: string;
  text: string;
  className?: string;
}

// LinkifiedText: render teks bebas apa adanya, tapi setiap substring yang
// berbentuk nomor dokumen dibungkus DocumentNumberLink supaya bisa diklik.
// Teks di sekitarnya tidak disentuh sama sekali.
export function LinkifiedText({ token, text, className }: Props) {
  const matches = [...text.matchAll(DOCUMENT_NUMBER_RE)];
  if (matches.length === 0) return <span className={className}>{text}</span>;

  const parts: ReactNode[] = [];
  let cursor = 0;
  matches.forEach((m, i) => {
    const start = m.index ?? 0;
    if (start > cursor) parts.push(<Fragment key={`t${i}`}>{text.slice(cursor, start)}</Fragment>);
    parts.push(
      <DocumentNumberLink
        key={`d${i}`}
        token={token}
        number={m[0]}
        className="font-mono text-accent hover:underline"
      />,
    );
    cursor = start + m[0].length;
  });
  if (cursor < text.length) parts.push(<Fragment key="tail">{text.slice(cursor)}</Fragment>);

  return <span className={className}>{parts}</span>;
}
