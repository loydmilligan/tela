import { cva } from 'class-variance-authority'

export const sheetVariants = cva(
  [
    'tela-sheet-content',
  ],
  {
    variants: {
      side: {
        right: 'tela-sheet-content--right',
        left: 'tela-sheet-content--left',
      },
    },
    defaultVariants: {
      side: 'right',
    },
  },
)
