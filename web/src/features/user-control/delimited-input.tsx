import { useEffect, useRef, useState, type ComponentProps } from 'react'

import { Input } from '@/components/ui/input'

type DelimitedInputProps = Omit<
  ComponentProps<typeof Input>,
  'value' | 'onChange'
> & {
  value: string[]
  onChange: (value: string[]) => void
}

// Keep the user's separators while editing, and publish parsed values on every
// keystroke so Enter and an external Save button both submit the latest value.
export function DelimitedInput(props: DelimitedInputProps) {
  const { value, onChange, ...inputProps } = props
  const [draft, setDraft] = useState(value.join(', '))
  const focused = useRef(false)
  useEffect(() => {
    if (!focused.current) setDraft(value.join(', '))
  }, [value])

  return (
    <Input
      {...inputProps}
      value={draft}
      onFocus={(event) => {
        focused.current = true
        props.onFocus?.(event)
      }}
      onBlur={(event) => {
        focused.current = false
        setDraft(value.join(', '))
        props.onBlur?.(event)
      }}
      onChange={(event) => {
        const text = event.target.value
        setDraft(text)
        onChange(
          text
            .split(/[,\n]/)
            .map((item) => item.trim())
            .filter(Boolean)
        )
      }}
    />
  )
}
