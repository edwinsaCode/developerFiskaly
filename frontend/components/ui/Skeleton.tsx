interface SkeletonProps {
  className?: string;
  height?: string;
  width?: string;
}

export function Skeleton({ className = "", height = "h-4", width = "w-full" }: SkeletonProps) {
  return (
    <div
      className={`rounded skeleton-pulse ${height} ${width} ${className}`}
      aria-hidden="true"
    />
  );
}

export function SkeletonCard({ lines = 3 }: { lines?: number }) {
  return (
    <div className="bg-surface border border-border rounded-lg p-5 space-y-3">
      <Skeleton height="h-5" width="w-1/3" />
      {Array.from({ length: lines }).map((_, i) => (
        <Skeleton key={i} height="h-4" width={i === lines - 1 ? "w-2/3" : "w-full"} />
      ))}
    </div>
  );
}

export function SkeletonMetric() {
  return (
    <div className="bg-surface border border-border rounded-lg p-5 space-y-2">
      <Skeleton height="h-3" width="w-24" />
      <Skeleton height="h-8" width="w-40" />
      <Skeleton height="h-3" width="w-20" />
    </div>
  );
}

export function SkeletonTable({ rows = 5, cols = 4 }: { rows?: number; cols?: number }) {
  return (
    <div className="space-y-px">
      <div className="grid gap-3 p-3 bg-border-subtle/40 border-b border-border"
        style={{ gridTemplateColumns: `repeat(${cols}, 1fr)` }}>
        {Array.from({ length: cols }).map((_, i) => (
          <Skeleton key={i} height="h-3" width="w-16" />
        ))}
      </div>
      {Array.from({ length: rows }).map((_, row) => (
        <div key={row} className="grid gap-3 p-3 border-b border-border-subtle last:border-0"
          style={{ gridTemplateColumns: `repeat(${cols}, 1fr)` }}>
          {Array.from({ length: cols }).map((_, col) => (
            <Skeleton key={col} height="h-4" width={col === cols - 1 ? "w-16 ml-auto" : "w-full"} />
          ))}
        </div>
      ))}
    </div>
  );
}
