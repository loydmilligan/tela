import { useMutation } from '@tanstack/react-query'
import { askDocs, type AskAnswer } from '../api'

// "Ask your docs" mutation. A question is a deliberate submit (not per-keystroke
// like search), so a mutation fits better than a query — the view fires it on
// Enter / button click and renders the single answer + cited sources.
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
