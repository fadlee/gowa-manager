import * as React from "react"
import { ChevronDown } from "lucide-react"

import { cn } from "../../lib/utils"

const Accordion = React.forwardRef<
  React.ElementRef<"div">,
  React.ComponentPropsWithoutRef<"div"> & {
    type?: "single" | "multiple"
    collapsible?: boolean
    defaultValue?: string | string[]
    value?: string | string[]
    onValueChange?: (value: string | string[]) => void
  }
>(({ className, type = "single", collapsible = false, defaultValue, value, onValueChange, children, ...props }, ref) => {
  const [openItems, setOpenItems] = React.useState<string[]>(
    defaultValue ? (Array.isArray(defaultValue) ? defaultValue : [defaultValue]) : []
  )

  const updateOpenItems = (itemValue: string, newState: boolean) => {
    if (type === "single" && !collapsible && openItems.includes(itemValue)) return

    setOpenItems((prev) => {
      const newOpenItems = prev.filter((item) => item !== itemValue)
      if (newState) newOpenItems.push(itemValue)
      return newOpenItems
    })
  }

  const isOpen = (itemValue: string) => openItems.includes(itemValue)

  return (
    <div ref={ref} className={cn("space-y-2", className)} {...props}>
      {React.Children.map(children, (child) => {
        if (!React.isValidElement(child)) return child

        const { value: itemValue, children: itemChildren } = child.props

        return React.cloneElement(child as React.ReactElement, {
          isOpen: isOpen(itemValue),
          onToggle: () => updateOpenItems(itemValue, !isOpen(itemValue)),
        })
      })}
    </div>
  )
})
Accordion.displayName = "Accordion"

const AccordionItem = React.forwardRef<
  React.ElementRef<"div">,
  React.ComponentPropsWithoutRef<"div"> & {
    value: string
    isOpen?: boolean
    onToggle?: () => void
  }
>(({ className, value, isOpen, onToggle, children, ...props }, ref) => (
  <div ref={ref} className={cn("rounded-md border border-gray-200", className)} {...props}>
    {React.Children.map(children, (child) => {
      if (React.isValidElement(child)) {
        return React.cloneElement(child, { isOpen, onToggle });
      }
      return child;
    })}
  </div>
))
AccordionItem.displayName = "AccordionItem"

const AccordionTrigger = React.forwardRef<
  React.ElementRef<"button">,
  React.ComponentPropsWithoutRef<"button"> & {
    isOpen?: boolean
    onToggle?: () => void
  }
>(({ className, children, isOpen, onToggle, ...props }, ref) => (
  <button
    type="button"
    className={cn(
      "flex flex-1 items-center justify-between w-full p-3 text-left transition-all hover:bg-gray-50 rounded-md [&[data-state=open]]:bg-gray-50",
      className
    )}
    onClick={onToggle}
    data-state={isOpen ? "open" : "closed"}
    {...props}
    ref={ref}
  >
    {children}
    <ChevronDown className={cn("h-4 w-4 transition-transform duration-200", isOpen && "rotate-180")} />
  </button>
))
AccordionTrigger.displayName = "AccordionTrigger"

const AccordionContent = React.forwardRef<
  React.ElementRef<"div">,
  React.ComponentPropsWithoutRef<"div"> & {
    isOpen?: boolean
  }
>(({ className, children, isOpen, ...props }, ref) => {
  // Measure content height and animate height directly to avoid relying on Radix CSS vars
  const innerRef = React.useRef<HTMLDivElement>(null)
  const [height, setHeight] = React.useState<number>(0)

  // Sync height on open/close and when content size changes
  React.useEffect(() => {
    const el = innerRef.current
    if (!el) return

    const update = () => {
      setHeight(isOpen ? el.scrollHeight : 0)
    }

    update()

    // Observe size changes of the content for dynamic height
    const ro = new ResizeObserver(update)
    ro.observe(el)

    // Also update on window resize just in case
    window.addEventListener("resize", update)

    return () => {
      ro.disconnect()
      window.removeEventListener("resize", update)
    }
  }, [isOpen, children])

  return (
    <div
      ref={ref}
      className={cn(
        // Transition height for smooth open/close
        "overflow-hidden transition-[height] duration-200 ease-out",
        className
      )}
      style={{ height }}
      data-state={isOpen ? "open" : "closed"}
      {...props}
    >
      <div ref={innerRef} className="pt-0 pb-4">
        {children}
      </div>
    </div>
  )
})
AccordionContent.displayName = "AccordionContent"

export { Accordion, AccordionItem, AccordionTrigger, AccordionContent }
