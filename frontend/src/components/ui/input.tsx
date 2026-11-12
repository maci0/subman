import * as React from "react"
import { cn } from "cn"

function Input({ className, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      data-slot="input"
      className={cn(
        "h-9 w-full min-w-0 rounded-control border border-input bg-transparent px-3 py-1 text-body transition-colors outline-none selection:bg-accent-soft selection:text-text file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-data file:font-medium file:text-foreground placeholder:text-muted-foreground disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-data",
        "focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-offset-2 focus-visible:outline-ring",
        "aria-invalid:border-destructive",
        className
      )}
      {...props}
    />
  )
}

export { Input }
