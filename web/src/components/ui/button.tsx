// Adapted from shadcn/ui Button (MIT), themed for Harbor. See public/shadcn-license.txt.
import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { Loader2 } from 'lucide-react'
import { cn } from '../../lib/utils'

const buttonVariants = cva('button', {
  variants: {
    variant: {
      primary: 'button-primary',
      secondary: 'button-secondary',
      ghost: 'button-ghost',
      danger: 'button-danger',
    },
  },
  defaultVariants: { variant: 'secondary' },
})

type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> & VariantProps<typeof buttonVariants> & { asChild?: boolean; busy?: boolean }

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, asChild = false, busy, disabled, children, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    return <Comp className={cn(buttonVariants({ variant, className }))} ref={ref} disabled={disabled || busy} aria-busy={busy || undefined} {...props}>
      {busy && !asChild && <Loader2 size={16} className="spin" aria-hidden="true" />}{children}
    </Comp>
  },
)
Button.displayName = 'Button'

export { Button, buttonVariants }
