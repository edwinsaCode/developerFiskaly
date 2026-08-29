// <Persen> — format angka persentase dari API.
// value: string dari API, misal "0.025" (= 2,5%)

interface PersenProps {
  /** Nilai decimal dari API, misal "0.025" atau "0.11". */
  value: string;
  /** Jumlah angka desimal yang ditampilkan. Default 2. */
  decimals?: number;
  className?: string;
}

export function Persen({ value, decimals = 2, className = "" }: PersenProps) {
  const numeric = parseFloat(value) * 100;
  const formatted = new Intl.NumberFormat("id-ID", {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(numeric);

  return (
    <span className={`tabular whitespace-nowrap ${className}`.trim()}>
      {formatted}%
    </span>
  );
}
