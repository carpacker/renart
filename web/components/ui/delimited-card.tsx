import * as React from "react";

import { cn } from "@/lib/utils";

function DelimitedCard({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="delimited-card"
      className={cn(
        "overflow-hidden rounded-xl border bg-card text-card-foreground shadow-sm",
        className,
      )}
      {...props}
    />
  );
}

function DelimitedCardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="delimited-card-header"
      className={cn("flex min-h-11 items-center gap-2 border-b bg-muted/30 px-3 py-2", className)}
      {...props}
    />
  );
}

function DelimitedCardTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="delimited-card-title"
      className={cn("min-w-0 truncate text-sm font-medium", className)}
      {...props}
    />
  );
}

function DelimitedCardDescription({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="delimited-card-description"
      className={cn("truncate text-xs text-muted-foreground", className)}
      {...props}
    />
  );
}

function DelimitedCardAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="delimited-card-action"
      className={cn("ml-auto flex shrink-0 items-center gap-1.5", className)}
      {...props}
    />
  );
}

function DelimitedCardContent({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="delimited-card-content" className={cn("p-3", className)} {...props} />;
}

function DelimitedCardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="delimited-card-footer"
      className={cn("border-t bg-muted/30 px-3 py-2", className)}
      {...props}
    />
  );
}

export {
  DelimitedCard,
  DelimitedCardAction,
  DelimitedCardContent,
  DelimitedCardDescription,
  DelimitedCardFooter,
  DelimitedCardHeader,
  DelimitedCardTitle,
};
