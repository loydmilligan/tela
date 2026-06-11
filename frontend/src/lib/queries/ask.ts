import { useMutation } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { askDocs, askDocsStream, type AskAnswer, type SemanticHit } from '../api'

// "Ask your docs" mutation (non-streaming). Kept for any caller that wants the
// whole answer in one shot; the /ask view uses useAskDocsStream below.
//
// Errors surface as ApiError on `error`: the view treats the 503 codes
// (rag_disabled / llm_disabled, see ASK_UNAVAILABLE_CODES) as a tasteful
// "feature not available on this instance" state, everything else as a retry.
// retry:false so a dark/unconfigured instance isn't hammered.
export function useAskDocs() {
  return useMutation<AskAnswer, Error, { question: string; spaceId?: number }>({
    mutationFn: ({ question, spaceId }) =>
      askDocs({ question, space_id: spaceId }),
    retry: false,
  })
}

export type AskStatus = 'idle' | 'streaming' | 'done' | 'error'

interface AskStreamState {
  status: AskStatus
  answer: string
  sources: SemanticHit[]
  lowConfidence: boolean
  followups: string[]
  error: unknown
}

const IDLE: AskStreamState = {
  status: 'idle',
  answer: '',
  sources: [],
  lowConfidence: false,
  followups: [],
  error: null,
}

// Streaming "Ask your docs". react-query's mutation models a single resolved
// value, which doesn't fit a token stream — so this is a small state machine over
// askDocsStream: it accumulates the answer as tokens land, surfaces sources the
// moment they arrive (before generation), and aborts the in-flight stream when a
// new question starts or the view unmounts.
export function useAskDocsStream() {
  const [state, setState] = useState<AskStreamState>(IDLE)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => () => abortRef.current?.abort(), [])

  const reset = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
    setState(IDLE)
  }, [])

  const ask = useCallback((question: string, spaceId?: number) => {
    abortRef.current?.abort()
    const ctrl = new AbortController()
    abortRef.current = ctrl
    setState({ ...IDLE, status: 'streaming' })
    void askDocsStream(
      { question, space_id: spaceId },
      {
        onSources: (sources, lowConfidence) =>
          setState((s) => ({ ...s, sources, lowConfidence })),
        onToken: (t) => setState((s) => ({ ...s, answer: s.answer + t })),
        onFollowups: (followups) => setState((s) => ({ ...s, followups })),
        onError: (err) => setState((s) => ({ ...s, status: 'error', error: err })),
        onDone: () =>
          setState((s) => (s.status === 'error' ? s : { ...s, status: 'done' })),
      },
      ctrl.signal,
    ).catch((err: unknown) => {
      if (ctrl.signal.aborted) return
      setState((s) => ({ ...s, status: 'error', error: err }))
    })
  }, [])

  return { ...state, ask, reset, isPending: state.status === 'streaming' }
}
