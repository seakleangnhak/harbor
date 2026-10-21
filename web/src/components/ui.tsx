import { type ButtonHTMLAttributes, type ReactNode } from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import * as DropdownPrimitive from '@radix-ui/react-dropdown-menu'
import * as TooltipPrimitive from '@radix-ui/react-tooltip'
import { Check, X } from 'lucide-react'
import { cn } from '../lib/utils'
export { cn } from '../lib/utils'
export { Button } from './ui/button'

export function IconButton({ label, children, className, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; children: ReactNode }) {
  return <TooltipPrimitive.Root><TooltipPrimitive.Trigger asChild>
    <button type="button" className={cn('icon-button', className)} aria-label={label} {...props}>{children}</button>
  </TooltipPrimitive.Trigger><TooltipPrimitive.Portal><TooltipPrimitive.Content className="tooltip" sideOffset={7}>{label}<TooltipPrimitive.Arrow className="tooltip-arrow" /></TooltipPrimitive.Content></TooltipPrimitive.Portal></TooltipPrimitive.Root>
}

export function Modal({ open, onOpenChange, title, description, children, className }: { open: boolean; onOpenChange: (open: boolean) => void; title: string; description?: string; children: ReactNode; className?: string }) {
  return <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}><DialogPrimitive.Portal>
    <DialogPrimitive.Overlay className="dialog-overlay" />
    <DialogPrimitive.Content className={cn('dialog-content', className)} {...(!description ? { 'aria-describedby': undefined } : {})}>
      <div className="dialog-heading"><div><DialogPrimitive.Title>{title}</DialogPrimitive.Title>{description && <DialogPrimitive.Description>{description}</DialogPrimitive.Description>}</div>
        <DialogPrimitive.Close asChild><button className="icon-button" aria-label="Close dialog"><X size={18} /></button></DialogPrimitive.Close>
      </div>{children}
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal></DialogPrimitive.Root>
}

export const Menu = DropdownPrimitive.Root
export const MenuTrigger = DropdownPrimitive.Trigger
export function MenuContent({ children, align = 'end' }: { children: ReactNode; align?: 'start' | 'end' }) {
  return <DropdownPrimitive.Portal><DropdownPrimitive.Content className="menu-content" align={align} sideOffset={6} collisionPadding={12}>{children}</DropdownPrimitive.Content></DropdownPrimitive.Portal>
}
export function MenuItem({ children, onSelect, danger, disabled }: { children: ReactNode; onSelect?: () => void; danger?: boolean; disabled?: boolean }) {
  return <DropdownPrimitive.Item className={cn('menu-item', danger && 'menu-danger')} onSelect={onSelect} disabled={disabled}>{children}</DropdownPrimitive.Item>
}
export const MenuSeparator = () => <DropdownPrimitive.Separator className="menu-separator" />

export function Checkbox({ checked, label, onChange, indeterminate }: { checked: boolean; label: string; onChange: () => void; indeterminate?: boolean }) {
  return <button type="button" role="checkbox" aria-checked={indeterminate ? 'mixed' : checked} aria-label={label} onClick={event => { event.stopPropagation(); onChange() }} className={cn('checkbox', (checked || indeterminate) && 'checkbox-checked')}>
    <span className="checkbox-indicator" aria-hidden="true">{indeterminate ? <span className="checkbox-dash" /> : checked && <Check size={12} strokeWidth={3} />}</span>
  </button>
}

export function InlineError({ children, id }: { children: ReactNode; id?: string }) { return <p id={id} className="inline-error" role="alert">{children}</p> }
