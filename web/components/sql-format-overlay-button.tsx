"use client";

import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";

type SqlFormatOverlayButtonProps = {
  visible: boolean;
  shortcutLabel: string;
  onFormat: () => void;
};

export function SqlFormatOverlayButton({
  visible,
  shortcutLabel,
  onFormat,
}: SqlFormatOverlayButtonProps) {
  const [shown, setShown] = useState(false);
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!visible) {
      setShown(false);
      if (hideTimerRef.current) {
        clearTimeout(hideTimerRef.current);
      }
      return;
    }

    setShown(true);
    if (hideTimerRef.current) {
      clearTimeout(hideTimerRef.current);
    }

    hideTimerRef.current = setTimeout(() => {
      setShown(false);
    }, 2200);

    return () => {
      if (hideTimerRef.current) {
        clearTimeout(hideTimerRef.current);
      }
    };
  }, [visible]);

  return (
    <div className="pointer-events-none absolute right-3 top-3 z-20">
      <Button
        className={`pointer-events-auto h-7 gap-2 px-2.5 shadow-sm transition-opacity duration-150 ${
          shown ? "opacity-100" : "opacity-0"
        }`}
        size="xs"
        type="button"
        variant="outline"
        title={`Format SQL (${shortcutLabel})`}
        onClick={onFormat}
      >
        <span>Format SQL</span>
        <span className="text-xs opacity-70">{shortcutLabel}</span>
      </Button>
    </div>
  );
}
