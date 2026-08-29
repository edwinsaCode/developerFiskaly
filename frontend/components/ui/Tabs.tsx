"use client";

import { createContext, useContext, useState, ReactNode } from "react";

interface TabsContextValue {
  active: string;
  setActive: (id: string) => void;
}

const TabsContext = createContext<TabsContextValue | null>(null);

interface TabsProps {
  defaultTab: string;
  children: ReactNode;
  className?: string;
}

export function Tabs({ defaultTab, children, className = "" }: TabsProps) {
  const [active, setActive] = useState(defaultTab);
  return (
    <TabsContext.Provider value={{ active, setActive }}>
      <div className={className}>{children}</div>
    </TabsContext.Provider>
  );
}

export function TabList({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    // Di layar sempit deretan tab digulir horizontal DI DALAM barisnya sendiri
    // (bukan memaksa seluruh halaman ikut melebar) dan label tidak dipatahkan
    // jadi dua baris.
    <div
      className={`flex overflow-x-auto scrollbar-none border-b border-border ${className}`}
      role="tablist"
    >
      {children}
    </div>
  );
}

interface TabTriggerProps {
  id: string;
  children: ReactNode;
}

export function TabTrigger({ id, children }: TabTriggerProps) {
  const ctx = useContext(TabsContext);
  if (!ctx) throw new Error("TabTrigger harus dalam Tabs");
  const isActive = ctx.active === id;
  return (
    <button
      role="tab"
      aria-selected={isActive}
      onClick={() => ctx.setActive(id)}
      className={`px-4 py-2.5 text-sm font-medium transition-colors border-b-2 -mb-px
        shrink-0 whitespace-nowrap
        ${isActive
          ? "border-accent text-accent"
          : "border-transparent text-text-secondary hover:text-accent hover:border-accent/30"
        }`}
    >
      {children}
    </button>
  );
}

interface TabPanelProps {
  id: string;
  children: ReactNode;
  className?: string;
}

export function TabPanel({ id, children, className = "" }: TabPanelProps) {
  const ctx = useContext(TabsContext);
  if (!ctx) throw new Error("TabPanel harus dalam Tabs");
  if (ctx.active !== id) return null;
  return (
    <div role="tabpanel" className={className}>
      {children}
    </div>
  );
}
