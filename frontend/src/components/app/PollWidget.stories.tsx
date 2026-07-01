import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { PollWidget, type PollData, type PollVoter } from './PollWidget'

const V = (id: number, name: string): PollVoter => ({ id, name })

const LISBON: PollVoter[] = [
  V(1, 'Ada Lovelace'),
  V(2, 'Alan Turing'),
  V(3, 'Grace Hopper'),
  V(4, 'Linus Torvalds'),
  V(5, 'Rob Pike'),
  V(6, 'Ken Thompson'),
  V(7, 'Barbara Liskov'),
]
const BERLIN: PollVoter[] = [
  V(8, 'Margaret Hamilton'),
  V(9, 'Katherine Johnson'),
  V(10, 'Donald Knuth'),
]
const BARCELONA: PollVoter[] = [V(11, 'Edsger Dijkstra'), V(12, 'Leslie Lamport')]

const base: PollData = {
  id: 'offsite',
  question: 'Where should we host the offsite?',
  options: [
    { id: 'lisbon', label: 'Lisbon', voters: LISBON, count: LISBON.length },
    { id: 'berlin', label: 'Berlin', voters: BERLIN, count: BERLIN.length },
    {
      id: 'barcelona',
      label: 'Barcelona',
      voters: BARCELONA,
      count: BARCELONA.length,
    },
  ],
  myChoice: null,
  allowChange: true,
  canVote: true,
}

const meta: Meta<typeof PollWidget> = {
  title: 'App/PollWidget',
  component: PollWidget,
  parameters: { layout: 'centered' },
  decorators: [
    (Story) => (
      <div style={{ width: 'calc(var(--space-8) * 12)', maxWidth: '92vw' }}>
        <Story />
      </div>
    ),
  ],
}
export default meta

type Story = StoryObj<typeof PollWidget>

// Interactive: casting a vote flips the caller's own tally + choice so the
// CHOOSE → RESULTS transition is real in the story (mirrors the optimistic
// update the live widget will do off the vote op).
function Interactive({ initial }: { initial: PollData }) {
  const [poll, setPoll] = useState(initial)
  return (
    <PollWidget
      poll={poll}
      onVote={(optionId) => {
        setPoll((p) => {
          const me = V(0, 'You')
          const cleared = p.options.map((o) => ({
            ...o,
            voters: o.voters.filter((v) => v.id !== 0),
            count: o.count - (o.voters.some((v) => v.id === 0) ? 1 : 0),
          }))
          if (!optionId) return { ...p, options: cleared, myChoice: null }
          return {
            ...p,
            myChoice: optionId,
            options: cleared.map((o) =>
              o.id === optionId
                ? { ...o, voters: [me, ...o.voters], count: o.count + 1 }
                : o,
            ),
          }
        })
      }}
    />
  )
}

// The default entry point: no results shown until you pick.
export const Choose: Story = {
  render: () => <Interactive initial={base} />,
}

export const Voted: Story = {
  render: () => <Interactive initial={{ ...base, myChoice: 'lisbon' }} />,
}

export const Anonymous: Story = {
  render: () => (
    <Interactive initial={{ ...base, secret: true, myChoice: 'berlin' }} />
  ),
}

export const Closed: Story = {
  args: {
    poll: { ...base, closed: true, myChoice: 'lisbon', allowChange: false },
  },
}

// A non-editor viewing an open poll: read-only results, no cast affordance.
export const ReadOnly: Story = {
  args: { poll: { ...base, canVote: false, myChoice: null } },
}

export const NoVotesYet: Story = {
  render: () => (
    <Interactive
      initial={{
        ...base,
        options: base.options.map((o) => ({ ...o, voters: [], count: 0 })),
      }}
    />
  ),
}

export const ManyVoters: Story = {
  render: () => (
    <Interactive
      initial={{
        ...base,
        myChoice: 'lisbon',
        options: [
          {
            id: 'lisbon',
            label: 'Lisbon',
            voters: Array.from({ length: 14 }, (_, i) =>
              V(20 + i, `Voter Number ${i + 1}`),
            ),
            count: 14,
          },
          ...base.options.slice(1),
        ],
      }}
    />
  ),
}
