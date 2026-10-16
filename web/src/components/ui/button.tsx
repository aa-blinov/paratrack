import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "cn"
import { Slot } from "radix-ui"

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-sm font-medium whitespace-nowrap transition-[color,background-color,border-color,outline-color,text-decoration-color,fill,stroke] outline-none select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90",
        outline:
          "border-border bg-background hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground dark:border-input dark:bg-white/[0.045] dark:hover:bg-input/50",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-[color-mix(in_oklch,var(--secondary),var(--foreground)_5%)] aria-expanded:bg-secondary aria-expanded:text-secondary-foreground",
        ghost:
          "hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground dark:hover:bg-muted/50",
        destructive:
          "bg-destructive/10 text-destructive hover:bg-destructive/20 focus-visible:border-destructive/40 focus-visible:ring-destructive/20 dark:bg-destructive/20 dark:hover:bg-destructive/30 dark:focus-visible:ring-destructive/40",
        link: "text-primary underline-offset-4 hover:underline",
      },
      // One radius per size, taken from the token scale: everything smaller
      // than the default action keeps the tighter --radius-md corner, the
      // default keeps --radius. Measured before this: at 32px the same
      // height carried both — the icon buttons were rounded at 10px while a
      // button resized to 32px elsewhere in the header was rounded at 8px,
      // and the two stood next to each other.
      //
      // The sizes below used to write that corner as
      // `rounded-[min(var(--radius-md),Npx)]`, which is not a clamp: with
      // --radius at 10px, --radius-md is 8px, and min(8,10) and min(8,12)
      // are the same 8px written two ways. The guard it pretended to give
      // could never fire.
      //
      // `lg` is gone. It was h-9, identical to `default` to the character —
      // a second name for the size that already had one, and unused.
      // `xs` and `icon-lg` were unused too. The scale is now 24 / 28 / 32 /
      // 36 with nothing between and nothing duplicated.
      size: {
        default:
          "h-9 gap-1.5 px-2.5 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2",
        sm: "h-7 gap-1 rounded-md px-2.5 text-[0.8rem] in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 [&_svg:not([class*='size-'])]:size-3.5",
        icon: "size-8 rounded-md",
        "icon-xs": "size-6 rounded-md [&_svg:not([class*='size-'])]:size-3",
        "icon-sm": "size-7 rounded-md",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot.Root : "button"

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

/**
 * A full-width row in the sidebar footer: language, theme, sign out.
 *
 * It used to be written out three times — theme toggle, language, logout —
 * each with its own copy of `h-8 … rounded-md`. Measured: that override is
 * what made `size="sm"` render at both 28px and 32px, and its `rounded-md`
 * fought the size's own corner. Repeating a control's shape in three places
 * is how they drift apart.
 *
 * The height stays because the row sits on the navigation line beside it; the
 * corner does not, because `size="sm"` already rounds at --radius-md and this
 * control is now 32px tall like the icon buttons in the header.
 */
const sidebarRow =
  "h-8 w-full justify-start gap-2 px-2 group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:justify-center"

export { Button, buttonVariants, sidebarRow }
