package voice

import "github.com/teanode/teanode/internal/providers"

func pruneVoiceContext(messages []providers.ChatMessage, maxTokens int) []providers.ChatMessage {
	if len(messages) == 0 || maxTokens <= 0 {
		return messages
	}

	estimateTokens := func(message providers.ChatMessage) int {
		switch content := message.Content.(type) {
		case string:
			return len(content)/4 + 1
		default:
			return 1
		}
	}

	// Separate the system prompt from conversation messages.
	systemMessages := make([]providers.ChatMessage, 0, 2)
	convMessages := make([]providers.ChatMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			systemMessages = append(systemMessages, message)
		} else {
			convMessages = append(convMessages, message)
		}
	}

	// Compute tokens consumed by system messages (always kept).
	systemTokens := 0
	for _, message := range systemMessages {
		systemTokens += estimateTokens(message)
	}

	remaining := maxTokens - systemTokens
	if remaining <= 0 {
		// System prompt alone exceeds budget — return as-is.
		return messages
	}

	// Always keep the last 2 conversation messages.
	minKeep := 2
	if len(convMessages) <= minKeep {
		return messages
	}

	guaranteed := convMessages[len(convMessages)-minKeep:]
	candidates := convMessages[:len(convMessages)-minKeep]

	// Count tokens in guaranteed messages.
	for _, message := range guaranteed {
		remaining -= estimateTokens(message)
	}

	// Walk candidates newest-first and include as many as fit.
	kept := make([]providers.ChatMessage, 0, len(candidates))
	for candidateIndex := len(candidates) - 1; candidateIndex >= 0; candidateIndex-- {
		tokens := estimateTokens(candidates[candidateIndex])
		if remaining-tokens < 0 {
			break
		}
		remaining -= tokens
		kept = append(kept, candidates[candidateIndex])
	}

	dropped := len(candidates) - len(kept)
	if dropped == 0 {
		return messages
	}

	// Reverse kept (we built it newest-first).
	for leftIndex, rightIndex := 0, len(kept)-1; leftIndex < rightIndex; leftIndex, rightIndex = leftIndex+1, rightIndex-1 {
		kept[leftIndex], kept[rightIndex] = kept[rightIndex], kept[leftIndex]
	}

	keptTotal := len(systemMessages) + len(kept) + len(guaranteed)
	estimatedKept := maxTokens - remaining
	pipelineLog.Debugf("voice context pruned: kept=%d dropped=%d estimated_tokens=%d", keptTotal, dropped, estimatedKept)

	result := make([]providers.ChatMessage, 0, keptTotal)
	result = append(result, systemMessages...)
	result = append(result, kept...)
	result = append(result, guaranteed...)
	return result
}
