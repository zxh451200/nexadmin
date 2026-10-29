import * as React from 'react'
import * as LabelPrimitive from '@radix-ui/react-label'
import { Slot } from '@radix-ui/react-slot'
import {
  Controller,
  FormProvider,
  useFormContext,
  type ControllerProps,
  type FieldPath,
  type FieldValues,
} from 'react-hook-form'
import { cn } from '@/lib/utils'
import { Label } from '@/framework/ui/label'

type RequiredKeys = Set<string> | null
const RequiredContext = React.createContext<RequiredKeys>(null)

/**
 * 由 zod schema 推出必填字段：拿当前值跑一遍 resolver，报错的键即视为必填。
 * 星号因此完全跟着校验规则走，新增/调整字段不用再逐处补 required。
 */
function useRequiredKeys(props: Record<string, unknown>): RequiredKeys {
  const [keys, setKeys] = React.useState<RequiredKeys>(null)
  const control = props.control as { _options?: { resolver?: unknown } } | undefined
  const getValues = props.getValues as (() => unknown) | undefined
  const resolver = control?._options?.resolver as
    | ((values: unknown, ctx: unknown, opts: unknown) => Promise<{ errors?: Record<string, unknown> }>)
    | undefined

  React.useEffect(() => {
    if (typeof resolver !== 'function') return
    let cancelled = false
    const values = typeof getValues === 'function' ? getValues() : {}
    Promise.resolve(resolver(values, undefined, { fields: {}, shouldUseNativeValidation: false, criteriaMode: 'firstError' }))
      .then((result) => {
        if (!cancelled) setKeys(new Set(Object.keys(result?.errors ?? {})))
      })
      .catch(() => {
        if (!cancelled) setKeys(new Set())
      })
    return () => {
      cancelled = true
    }
  }, [resolver, getValues])

  return keys
}

function FormInner({ children, ...rest }: { children?: React.ReactNode } & Record<string, unknown>) {
  const keys = useRequiredKeys(rest)
  return (
    <RequiredContext.Provider value={keys}>
      <FormProvider {...(rest as unknown as React.ComponentProps<typeof FormProvider>)}>{children}</FormProvider>
    </RequiredContext.Provider>
  )
}

/** 与 FormProvider 同签名，多包一层「必填字段」上下文 */
const Form = FormInner as unknown as typeof FormProvider

type FormFieldContextValue<
  TFieldValues extends FieldValues = FieldValues,
  TName extends FieldPath<TFieldValues> = FieldPath<TFieldValues>,
> = { name: TName }

const FormFieldContext = React.createContext<FormFieldContextValue>({} as FormFieldContextValue)

const FormField = <
  TFieldValues extends FieldValues = FieldValues,
  TName extends FieldPath<TFieldValues> = FieldPath<TFieldValues>,
>({
  ...props
}: ControllerProps<TFieldValues, TName>) => (
  <FormFieldContext.Provider value={{ name: props.name }}>
    <Controller {...props} />
  </FormFieldContext.Provider>
)

type FormItemContextValue = { id: string; required: boolean }
const FormItemContext = React.createContext<FormItemContextValue>({} as FormItemContextValue)

const useFormField = () => {
  const fieldContext = React.useContext(FormFieldContext)
  const itemContext = React.useContext(FormItemContext)
  const { getFieldState, formState } = useFormContext()

  const fieldState = getFieldState(fieldContext.name, formState)

  if (!fieldContext) throw new Error('useFormField should be used within <FormField>')

  const { id, required } = itemContext
  return {
    id,
    required,
    name: fieldContext.name,
    formItemId: `${id}-form-item`,
    formDescriptionId: `${id}-form-item-description`,
    formMessageId: `${id}-form-item-message`,
    ...fieldState,
  }
}

const FormItem = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement> & { required?: boolean }>(
  ({ className, required, ...props }, ref) => {
    const id = React.useId()
    const { name } = React.useContext(FormFieldContext)
    const keys = React.useContext(RequiredContext)
    // 显式 required 优先；否则按 schema 推出来的必填集合判断
    const isRequired = required ?? (keys?.has(String(name)) ?? false)
    return (
      <FormItemContext.Provider value={{ id, required: isRequired }}>
        <div ref={ref} className={cn('space-y-2', className)} {...props} />
      </FormItemContext.Provider>
    )
  },
)
FormItem.displayName = 'FormItem'

const FormLabel = React.forwardRef<
  React.ElementRef<typeof LabelPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root>
>(({ className, children, ...props }, ref) => {
  const { formItemId, required } = useFormField()
  return (
    <Label ref={ref} className={cn(className)} htmlFor={formItemId} {...props}>
      {children}
      {required ? (
        <span aria-hidden="true" className="ms-0.5 text-destructive">
          *
        </span>
      ) : null}
    </Label>
  )
})
FormLabel.displayName = 'FormLabel'

const FormControl = React.forwardRef<React.ElementRef<typeof Slot>, React.ComponentPropsWithoutRef<typeof Slot>>(
  ({ ...props }, ref) => {
    const { error, required, formItemId, formDescriptionId, formMessageId } = useFormField()
    return (
      <Slot
        ref={ref}
        id={formItemId}
        aria-describedby={!error ? formDescriptionId : `${formDescriptionId} ${formMessageId}`}
        aria-invalid={!!error}
        aria-required={required || undefined}
        {...props}
      />
    )
  },
)
FormControl.displayName = 'FormControl'

const FormDescription = React.forwardRef<HTMLParagraphElement, React.HTMLAttributes<HTMLParagraphElement>>(
  ({ className, ...props }, ref) => {
    const { formDescriptionId } = useFormField()
    return <p ref={ref} id={formDescriptionId} className={cn('text-[0.8rem] text-muted-foreground', className)} {...props} />
  },
)
FormDescription.displayName = 'FormDescription'

const FormMessage = React.forwardRef<HTMLParagraphElement, React.HTMLAttributes<HTMLParagraphElement>>(
  ({ className, children, ...props }, ref) => {
    const { error, formMessageId } = useFormField()
    const body = error ? String(error?.message ?? '') : children
    if (!body) return null
    return (
      <p ref={ref} id={formMessageId} className={cn('text-[0.8rem] font-medium text-destructive', className)} {...props}>
        {body}
      </p>
    )
  },
)
FormMessage.displayName = 'FormMessage'

export { useFormField, Form, FormItem, FormLabel, FormControl, FormDescription, FormMessage, FormField }
