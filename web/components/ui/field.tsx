import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

type DivProps = React.HTMLAttributes<HTMLDivElement>;
type LabelProps = React.ComponentProps<typeof Label>;

export function FieldGroup({ className, ...props }: DivProps) {
  return <div className={cn("grid gap-3", className)} {...props} />;
}

export function FieldLabel({ className, ...props }: LabelProps) {
  return <Label className={cn("block cursor-pointer", className)} {...props} />;
}

export function Field({
  className,
  orientation = "vertical",
  variant = "default",
  ...props
}: DivProps & {
  orientation?: "horizontal" | "vertical";
  variant?: "default" | "plain";
}) {
  return (
    <div
      className={cn(
        variant === "default" ? "rounded-lg border px-3 py-3" : "p-0",
        orientation === "horizontal" ? "flex items-start justify-between gap-3" : "grid gap-3",
        className,
      )}
      {...props}
    />
  );
}

export function FieldContent({ className, ...props }: DivProps) {
  return <div className={cn("flex min-w-0 flex-col gap-1", className)} {...props} />;
}

export function FieldTitle({ className, ...props }: DivProps) {
  return <div className={cn("text-sm font-medium", className)} {...props} />;
}

export function FieldDescription({ className, ...props }: DivProps) {
  return <div className={cn("text-sm text-muted-foreground", className)} {...props} />;
}
