import type { ComponentProps } from "react"
import { DropdownMenu as Primitive } from "radix-ui"
import { cn } from "cn"
export const DropdownMenu = Primitive.Root
export const DropdownMenuTrigger = Primitive.Trigger
export const DropdownMenuLabel = Primitive.Label
export function DropdownMenuContent({className,...props}:ComponentProps<typeof Primitive.Content>) {
  return <Primitive.Portal container={document.getElementById("paratrack-react-root") ?? undefined}><Primitive.Content sideOffset={6} className={cn("z-50 min-w-48 max-w-[calc(100vw-2rem)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-md",className)} {...props}/></Primitive.Portal>
}
export function DropdownMenuItem({className,...props}:ComponentProps<typeof Primitive.Item>) {
  return <Primitive.Item onSelect={event=>{if((event.target as HTMLElement).closest('button[type="submit"]'))event.preventDefault()}} className={cn("relative flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-sm outline-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",className)} {...props}/>
}
export function DropdownMenuSeparator(props:ComponentProps<typeof Primitive.Separator>) {return <Primitive.Separator className="my-1 h-px bg-border" {...props}/>}
